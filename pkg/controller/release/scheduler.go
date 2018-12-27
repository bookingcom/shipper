package release

import (
	"sort"
	"strings"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/record"
	helmchart "k8s.io/helm/pkg/proto/hapi/chart"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperchart "github.com/bookingcom/shipper/pkg/chart"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/controller"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

// Scheduler is an object that knows how to schedule releases.
type Scheduler struct {
	Release *shipper.Release

	shipperclientset clientset.Interface

	clustersLister           listers.ClusterLister
	trafficTargetLister      listers.TrafficTargetLister
	installationTargetLister listers.InstallationTargetLister
	capacityTargetLister     listers.CapacityTargetLister

	fetchChart shipperchart.FetchFunc
	recorder   record.EventRecorder
}

// NewScheduler returns a new Scheduler instance that knows how to
// schedule a particular Release.
func NewScheduler(
	release *shipper.Release,
	shipperclientset clientset.Interface,
	clusterLister listers.ClusterLister,
	installationTargetLister listers.InstallationTargetLister,
	capacityTargetLister listers.CapacityTargetLister,
	trafficTargetLister listers.TrafficTargetLister,
	chartFetchFunc shipperchart.FetchFunc,
	recorder record.EventRecorder,
) *Scheduler {
	return &Scheduler{
		Release: release.DeepCopy(),

		shipperclientset: shipperclientset,

		clustersLister:           clusterLister,
		installationTargetLister: installationTargetLister,
		capacityTargetLister:     capacityTargetLister,
		trafficTargetLister:      trafficTargetLister,

		fetchChart: chartFetchFunc,
		recorder:   recorder,
	}
}

func (s *Scheduler) ScheduleRelease() error {
	glog.Infof("Processing release %q", controller.MetaKey(s.Release))
	defer glog.Infof("Finished processing %q", controller.MetaKey(s.Release))

	// Compute target clusters, and update the release if it doesn't have any.
	if !s.HasClusters() {
		clusterList, err := s.clustersLister.List(labels.Everything())
		if err != nil {
			return NewFailedAPICallError("ListClusters", err)
		}

		clusters, err := computeTargetClusters(s.Release, clusterList)
		if err != nil {
			return err
		}

		s.SetClusters(clusters)
		newRelease, err := s.UpdateRelease()
		if err != nil {
			return NewFailedAPICallError("UpdateRelease", err)
		}
		s.Release = newRelease

		s.recorder.Eventf(
			s.Release,
			corev1.EventTypeNormal,
			"ClustersSelected",
			"Set clusters for %q to %v",
			controller.MetaKey(s.Release),
			clusters,
		)
	}

	replicaCount, err := s.fetchChartAndExtractReplicaCount()
	if err != nil {
		return err
	}

	if err := s.CreateInstallationTarget(); err != nil {
		return err
	}

	if err := s.CreateTrafficTarget(); err != nil {
		return err
	}

	if err := s.CreateCapacityTarget(replicaCount); err != nil {
		return err
	}

	// If we get to this point, it means that the clusters have already been
	// selected and persisted in the Release, and all the associated Releases have
	// already been created.
	condition := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&s.Release.Status, *condition)

	if len(s.Release.Status.Conditions) == 0 {
		glog.Errorf(
			"Conditions don't seem right here for Release %q",
			controller.MetaKey(s.Release))
	}

	_, err = s.UpdateRelease()
	return err
}

func (s *Scheduler) HasClusters() bool {
	return len(s.Release.Annotations[shipper.ReleaseClustersAnnotation]) > 0
}

func (s *Scheduler) Clusters() []string {
	clusters := strings.Split(s.Release.Annotations[shipper.ReleaseClustersAnnotation], ",")
	if len(clusters) == 1 && clusters[0] == "" {
		clusters = []string{}
	}

	return clusters
}

func (s *Scheduler) SetClusters(clusters []string) {
	sort.Strings(clusters)
	s.Release.Annotations[shipper.ReleaseClustersAnnotation] = strings.Join(clusters, ",")
}

func (s *Scheduler) UpdateRelease() (*shipper.Release, error) {
	return s.shipperclientset.ShipperV1alpha1().Releases(s.Release.Namespace).Update(s.Release)
}

func (s *Scheduler) fetchChartAndExtractReplicaCount() (int32, error) {
	chart, err := s.fetchChart(s.Release.Spec.Environment.Chart)
	if err != nil {
		return 0, NewChartFetchFailureError(
			s.Release.Spec.Environment.Chart.Name,
			s.Release.Spec.Environment.Chart.Version,
			s.Release.Spec.Environment.Chart.RepoURL,
			err,
		)
	}

	replicas, err := s.extractReplicasFromChart(chart)
	if err != nil {
		return 0, err
	}

	glog.V(4).Infof("Extracted %v replicas from release %q", replicas,
		controller.MetaKey(s.Release))

	return int32(replicas), nil
}

func (s *Scheduler) extractReplicasFromChart(chart *helmchart.Chart) (int32, error) {
	owners := s.Release.OwnerReferences
	if l := len(owners); l != 1 {
		return 0, NewInvalidReleaseOwnerRefsError(len(owners))
	}

	applicationName := owners[0].Name
	rendered, err := shipperchart.Render(chart, applicationName, s.Release.Namespace, s.Release.Spec.Environment.Values)
	if err != nil {
		return 0, NewBrokenChartError(
			s.Release.Spec.Environment.Chart.Name,
			s.Release.Spec.Environment.Chart.Version,
			s.Release.Spec.Environment.Chart.RepoURL,
			err,
		)
	}

	deployments := shipperchart.GetDeployments(rendered)
	if len(deployments) != 1 {
		return 0, NewWrongChartDeploymentsError(
			s.Release.Spec.Environment.Chart.Name,
			s.Release.Spec.Environment.Chart.Version,
			s.Release.Spec.Environment.Chart.RepoURL,
			len(deployments),
		)
	}

	replicas := deployments[0].Spec.Replicas
	// Deployments default to 1 replica when replicas is nil or unspecified. See
	// k8s.io/api/apps/v1/types.go's DeploymentSpec.
	if replicas == nil {
		return 1, nil
	}

	return int32(*replicas), nil
}

// CreateInstallationTarget creates a new InstallationTarget object for
// Scheduler's Release property. Returns an error if the object couldn't
// be created.
func (s *Scheduler) CreateInstallationTarget() error {
	installationTarget := &shipper.InstallationTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.Release.Name,
			Namespace: s.Release.Namespace,
			Labels:    s.Release.Labels,
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromRelease(s.Release),
			},
		},
		Spec: shipper.InstallationTargetSpec{Clusters: s.Clusters()},
	}

	_, err := s.shipperclientset.ShipperV1alpha1().InstallationTargets(s.Release.Namespace).Create(installationTarget)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			installationTarget, listerErr := s.installationTargetLister.InstallationTargets(s.Release.Namespace).Get(s.Release.Name)
			if listerErr != nil {
				glog.Errorf("Failed to fetch installation target: %s", listerErr)
				return listerErr
			}

			for _, ownerRef := range installationTarget.GetOwnerReferences() {
				if ownerRef.UID == s.Release.UID {
					glog.Infof("InstallationTarget %q already exists but"+
						" it belongs to current release, proceeding normally",
						controller.MetaKey(s.Release))
					return nil
				}
			}

			glog.Errorf("InstallationTarget %q already exists and it does not"+
				" belong to the current release, bailing out", controller.MetaKey(installationTarget))

			return err
		}
		return NewFailedAPICallError("CreateInstallationTarget", err)
	}

	s.recorder.Eventf(
		s.Release,
		corev1.EventTypeNormal,
		"ReleaseScheduled",
		"Created InstallationTarget %q",
		controller.MetaKey(installationTarget),
	)

	return nil
}

// CreateCapacityTarget creates a new CapacityTarget object for Scheduler's
// Release property. Returns an error if the object couldn't be created.
func (s *Scheduler) CreateCapacityTarget(totalReplicaCount int32) error {
	count := len(s.Clusters())
	targets := make([]shipper.ClusterCapacityTarget, count)
	for i, v := range s.Clusters() {
		targets[i] = shipper.ClusterCapacityTarget{Name: v, Percent: 0, TotalReplicaCount: totalReplicaCount}
	}
	capacityTarget := &shipper.CapacityTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.Release.Name,
			Namespace: s.Release.Namespace,
			Labels:    s.Release.Labels,
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromRelease(s.Release),
			},
		},
		Spec: shipper.CapacityTargetSpec{
			Clusters: targets,
		},
	}

	_, err := s.shipperclientset.ShipperV1alpha1().CapacityTargets(s.Release.Namespace).Create(capacityTarget)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			capacityTarget, listerErr := s.capacityTargetLister.CapacityTargets(s.Release.Namespace).Get(s.Release.Name)
			if listerErr != nil {
				glog.Errorf("Failed to fetch capacity target: %s", listerErr)
				return listerErr
			}
			for _, ownerRef := range capacityTarget.GetOwnerReferences() {
				if ownerRef.UID == s.Release.UID {
					glog.Infof("CapacityTarget %q already exists but"+
						" it belongs to current release, proceeding normally",
						controller.MetaKey(s.Release))
					return nil
				}
			}
			glog.Errorf("CapacityTarget %q already exists and it does not"+
				" belong to the current release, bailing out", controller.MetaKey(capacityTarget))

			return err
		}
		return NewFailedAPICallError("CreateCapacityTarget", err)
	}

	s.recorder.Eventf(
		s.Release,
		corev1.EventTypeNormal,
		"ReleaseScheduled",
		"Created CapacityTarget %q",
		controller.MetaKey(capacityTarget),
	)

	return nil
}

// CreateTrafficTarget creates a new TrafficTarget object for Scheduler's
// Release property. Returns an error if the object couldn't be created.
func (s *Scheduler) CreateTrafficTarget() error {
	count := len(s.Clusters())
	trafficTargets := make([]shipper.ClusterTrafficTarget, count)
	for i, v := range s.Clusters() {
		trafficTargets[i] = shipper.ClusterTrafficTarget{Name: v, Weight: 0}
	}

	trafficTarget := &shipper.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.Release.Name,
			Namespace: s.Release.Namespace,
			Labels:    s.Release.Labels,
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromRelease(s.Release),
			},
		},
		Spec: shipper.TrafficTargetSpec{Clusters: trafficTargets},
	}

	_, err := s.shipperclientset.ShipperV1alpha1().TrafficTargets(s.Release.Namespace).Create(trafficTarget)
	if err != nil {
		if errors.IsAlreadyExists(err) {

			trafficTarget, listerErr := s.trafficTargetLister.TrafficTargets(s.Release.Namespace).Get(s.Release.Name)
			if listerErr != nil {
				glog.Errorf("Failed to fetch traffic target: %s", listerErr)
				return listerErr
			}
			for _, ownerRef := range trafficTarget.GetOwnerReferences() {
				if ownerRef.UID == s.Release.UID {
					glog.Infof("TrafficTarget %q already exists but"+
						" it belongs to current release, proceeding normally",
						controller.MetaKey(s.Release))
					return nil
				}
			}
			glog.Errorf("TrafficTarget %q already exists and it does not"+
				" belong to the current release, bailing out", controller.MetaKey(trafficTarget))

			return err
		}
		return NewFailedAPICallError("CreateTrafficTarget", err)
	}

	s.recorder.Eventf(
		s.Release,
		corev1.EventTypeNormal,
		"ReleaseScheduled",
		"Created TrafficTarget %q",
		controller.MetaKey(trafficTarget),
	)

	return nil
}

// computeTargetClusters picks out the clusters from the given list which match
// the release's clusterRequirements.
func computeTargetClusters(release *shipper.Release, clusterList []*shipper.Cluster) ([]string, error) {
	regionSpecs := release.Spec.Environment.ClusterRequirements.Regions
	requiredCapabilities := release.Spec.Environment.ClusterRequirements.Capabilities
	capableClustersByRegion := map[string][]*shipper.Cluster{}
	regionReplicas := map[string]int{}

	if len(regionSpecs) == 0 {
		return nil, NewNoRegionsSpecifiedError()
	}

	app, err := releaseutil.ApplicationNameForRelease(release)
	if err != nil {
		return nil, err
	}

	err = validateClusterRequirements(release.Spec.Environment.ClusterRequirements)
	if err != nil {
		return nil, err
	}

	prefList := buildPrefList(app, clusterList)
	// This algo could probably build up hashes instead of doing linear searches,
	// but these data sets are so tiny (1-20 items) that it'd only be useful for
	// readability.
	for _, region := range regionSpecs {
		capableClustersByRegion[region.Name] = []*shipper.Cluster{}
		if region.Replicas == nil {
			regionReplicas[region.Name] = 1
		} else {
			regionReplicas[region.Name] = int(*region.Replicas)
		}

		matchedRegion := 0
		for _, cluster := range prefList {
			if cluster.Spec.Scheduler.Unschedulable {
				continue
			}

			if cluster.Spec.Region == region.Name {
				matchedRegion++
				capabilityMatch := 0
				for _, requiredCapability := range requiredCapabilities {
					for _, providedCapability := range cluster.Spec.Capabilities {
						if requiredCapability == providedCapability {
							capabilityMatch++
							break
						}
					}
				}

				if capabilityMatch == len(requiredCapabilities) {
					capableClustersByRegion[region.Name] = append(capableClustersByRegion[region.Name], cluster)
				}
			}
		}
		if regionReplicas[region.Name] > matchedRegion {
			return nil, NewNotEnoughClustersInRegionError(region.Name, regionReplicas[region.Name], matchedRegion)
		}
	}

	clusterNames := []string{}
	for region, clusters := range capableClustersByRegion {
		if regionReplicas[region] > len(clusters) {
			return nil, NewNotEnoughCapableClustersInRegionError(
				region,
				requiredCapabilities,
				regionReplicas[region],
				len(clusters),
			)
		}

		//NOTE(btyler): this assumes we do not have duplicate cluster names. For the
		//moment cluster objects are cluster scoped; if they become namespace scoped
		//and releases can somehow be scheduled to clusters from multiple namespaces,
		//this assumption will be wrong.
		for _, cluster := range clusters {
			if regionReplicas[region] > 0 {
				regionReplicas[region]--
				clusterNames = append(clusterNames, cluster.Name)
			}
		}
	}

	sort.Strings(clusterNames)
	return clusterNames, nil
}

func validateClusterRequirements(requirements shipper.ClusterRequirements) error {
	// Ensure capability uniqueness. Erroring instead of de-duping in order to
	// avoid second-guessing by operators about how Shipper might treat repeated
	// listings of the same capability.
	seenCapabilities := map[string]struct{}{}
	for _, capability := range requirements.Capabilities {
		_, ok := seenCapabilities[capability]
		if ok {
			return NewDuplicateCapabilityRequirementError(capability)
		}
		seenCapabilities[capability] = struct{}{}
	}

	return nil
}

// The strings here are insane, but if you create a fresh release object for
// some reason it lands in the work queue with an empty TypeMeta. This is
// resolved if you restart the controllers, so I'm not sure what's going on.
// https://github.com/kubernetes/client-go/issues/60#issuecomment-281533822 and
// https://github.com/kubernetes/client-go/issues/60#issuecomment-281747911 give
// some potential context.
func createOwnerRefFromRelease(r *shipper.Release) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: shipper.SchemeGroupVersion.String(),
		Kind:       "Release",
		Name:       r.GetName(),
		UID:        r.GetUID(),
	}
}
