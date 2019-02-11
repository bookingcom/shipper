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
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/controller"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

type Scheduler struct {
	clientset shipperclientset.Interface

	clusterLister            listers.ClusterLister
	installationTargetLister listers.InstallationTargetLister
	trafficTargetLister      listers.TrafficTargetLister
	capacityTargetLister     listers.CapacityTargetLister

	fetchChart shipperchart.FetchFunc
	recorder   record.EventRecorder
}

func NewScheduler(
	clientset shipperclientset.Interface,
	clusterLister listers.ClusterLister,
	installationTargerLister listers.InstallationTargetLister,
	trafficTargetLister listers.TrafficTargetLister,
	capacityTargetLister listers.CapacityTargetLister,
	fetchChart shipperchart.FetchFunc,
	recorder record.EventRecorder,
) *Scheduler {
	return &Scheduler{
		clientset: clientset,

		clusterLister:            clusterLister,
		installationTargetLister: installationTargerLister,
		trafficTargetLister:      trafficTargetLister,
		capacityTargetLister:     capacityTargetLister,

		fetchChart: fetchChart,
		recorder:   recorder,
	}
}

func (s *Scheduler) ScheduleRelease(origrel *shipper.Release) (*shipper.Release, error) {

	rel := origrel.DeepCopy()

	metaKey := controller.MetaKey(rel)
	glog.Infof("Processing release %q", metaKey)
	defer glog.Infof("Finished processing %q", metaKey)

	if !ReleaseHasClusters(rel) {
		allClusters, err := s.clusterLister.List(labels.Everything())
		if err != nil {
			return nil, NewFailedAPICallError("ListClsuters", err)
		}
		selectedClusters, err := computeTargetClusters(rel, allClusters)
		if err != nil {
			return nil, err
		}
		setClusters(rel, selectedClusters)

		rel, err := s.clientset.ShipperV1alpha1().Releases(rel.Namespace).Update(rel)
		if err != nil {
			return nil, NewFailedAPICallError("UpdateRelease", err)
		}

		s.recorder.Eventf(
			rel,
			corev1.EventTypeNormal,
			"ClustersSelected",
			"Set clusters for %q to %v",
			metaKey,
			rel.Annotations[shipper.ReleaseClustersAnnotation],
		)
	}

	replicaCount, err := s.fetchChartAndExtractReplicaCount(rel)
	if err != nil {
		return nil, err
	}

	if _, err := s.CreateInstallationTarget(rel); err != nil {
		return nil, err
	}

	if _, err := s.CreateTrafficTarget(rel); err != nil {
		return nil, err
	}

	if _, err := s.CreateCapacityTarget(rel, replicaCount); err != nil {
		return nil, err
	}

	// If we get to this point, it means that the clusters have already been
	// selected and persisted in the Release, and all the associated Releases have
	// already been created.
	condition := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&rel.Status, *condition)

	if len(rel.Status.Conditions) == 0 {
		glog.Errorf(
			"Conditions don't see right here for Release %q",
			metaKey,
		)
	}

	return s.clientset.ShipperV1alpha1().Releases(rel.Namespace).Update(rel)
}

func ReleaseHasClusters(rel *shipper.Release) bool {
	return len(rel.Annotations[shipper.ReleaseClustersAnnotation]) > 0
}

func (s *Scheduler) CreateInstallationTarget(rel *shipper.Release) (*shipper.InstallationTarget, error) {
	clusterNames := strings.Split(rel.Annotations[shipper.ReleaseClustersAnnotation], ",")
	if len(clusterNames) == 1 && clusterNames[0] == "" {
		clusterNames = []string{}
	}

	it := &shipper.InstallationTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rel.Name,
			Namespace: rel.Namespace,
			Labels:    rel.Labels,
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromRelease(rel),
			},
		},
		Spec: shipper.InstallationTargetSpec{Clusters: clusterNames},
	}

	installationTarget, err := s.clientset.ShipperV1alpha1().InstallationTargets(rel.Namespace).Create(it)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			installationTarget, listerErr := s.installationTargetLister.InstallationTargets(rel.Namespace).Get(rel.Name)
			if listerErr != nil {
				glog.Errorf("Failed to fetch isntallation target: %s", listerErr)
				return nil, listerErr
			}

			for _, ownerRef := range installationTarget.GetOwnerReferences() {
				if ownerRef.UID == rel.UID {
					glog.Infof("InstallationTarget %q already exists but"+
						" it belongs to current release, proceeding normally",
						controller.MetaKey(rel))
					return installationTarget, nil
				}
			}

			glog.Errorf("InstallationTarget %q already exists and it does not"+
				" belong to the current release, bailing out", controller.MetaKey(installationTarget))

			return nil, err
		}

		return nil, NewFailedAPICallError("CreateInstallationTarget", err)
	}

	s.recorder.Eventf(
		rel,
		corev1.EventTypeNormal,
		"ReleaseScheduled",
		"Created InstallationTarget %q",
		controller.MetaKey(installationTarget),
	)

	return installationTarget, nil
}

func (s *Scheduler) CreateTrafficTarget(rel *shipper.Release) (*shipper.TrafficTarget, error) {
	clusterNames := strings.Split(rel.Annotations[shipper.ReleaseClustersAnnotation], ",")
	if len(clusterNames) == 1 && clusterNames[0] == "" {
		clusterNames = []string{}
	}

	trafficTargetClusters := make([]shipper.ClusterTrafficTarget, 0, len(clusterNames))
	for _, clusterName := range clusterNames {
		trafficTargetClusters = append(
			trafficTargetClusters,
			shipper.ClusterTrafficTarget{
				Name:   clusterName,
				Weight: 0,
			})
	}

	tt := &shipper.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rel.Name,
			Namespace: rel.Namespace,
			Labels:    rel.Labels,
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromRelease(rel),
			},
		},
		Spec: shipper.TrafficTargetSpec{Clusters: trafficTargetClusters},
	}

	trafficTarget, err := s.clientset.ShipperV1alpha1().TrafficTargets(rel.Namespace).Create(tt)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			trafficTarget, listerErr := s.trafficTargetLister.TrafficTargets(rel.Namespace).Get(rel.Name)
			if listerErr != nil {
				glog.Errorf("Failed to fetch traffic target: %s", listerErr)
				return nil, listerErr
			}
			for _, ownerRef := range trafficTarget.GetOwnerReferences() {
				if ownerRef.UID == rel.UID {
					glog.Infof("TrafficTarget %q already exists but"+
						" it belongs to current release, proceeding normally",
						controller.MetaKey(rel))
					return trafficTarget, nil
				}
			}
			glog.Errorf("TrafficTarget %q already exists and it does not"+
				" belong to the current release, bailing out", controller.MetaKey(trafficTarget))
			return nil, err
		}
		return nil, NewFailedAPICallError("CreateTrafficTarget", err)
	}

	s.recorder.Eventf(
		rel,
		corev1.EventTypeNormal,
		"ReleaseScheduled",
		"Created TrafficTarget %q",
		controller.MetaKey(trafficTarget),
	)

	return trafficTarget, nil
}

func (s *Scheduler) CreateCapacityTarget(rel *shipper.Release, totalReplicaCount int32) (*shipper.CapacityTarget, error) {
	clusterNames := strings.Split(rel.Annotations[shipper.ReleaseClustersAnnotation], ",")
	if len(clusterNames) == 1 && clusterNames[0] == "" {
		clusterNames = []string{}
	}
	capacityTargetClusters := make([]shipper.ClusterCapacityTarget, 0, len(clusterNames))
	for _, clusterName := range clusterNames {
		capacityTargetClusters = append(
			capacityTargetClusters,
			shipper.ClusterCapacityTarget{
				Name:              clusterName,
				Percent:           0,
				TotalReplicaCount: totalReplicaCount,
			})
	}

	ct := &shipper.CapacityTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rel.Name,
			Namespace: rel.Namespace,
			Labels:    rel.Labels,
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromRelease(rel),
			},
		},
		Spec: shipper.CapacityTargetSpec{
			Clusters: capacityTargetClusters,
		},
	}

	capacityTarget, err := s.clientset.ShipperV1alpha1().CapacityTargets(rel.Namespace).Create(ct)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			capacityTarget, listerErr := s.capacityTargetLister.CapacityTargets(rel.Namespace).Get(rel.Name)
			if listerErr != nil {
				glog.Errorf("Failed to fetch capacity target: %s", listerErr)
				return nil, listerErr
			}
			for _, ownerRef := range capacityTarget.GetOwnerReferences() {
				if ownerRef.UID == rel.UID {
					glog.Infof("CapacityTarget %q already exists but"+
						" it belongs to current release, proceeding normally",
						controller.MetaKey(rel))
					return capacityTarget, nil
				}
			}
			glog.Errorf("CapacityTarget %q already exists and it does not"+
				" belong to the current release, bailing out", controller.MetaKey(capacityTarget))

			return nil, err
		}
		return nil, NewFailedAPICallError("CreateCapacityTarget", err)
	}

	s.recorder.Eventf(
		rel,
		corev1.EventTypeNormal,
		"ReleaseScheduled",
		"Created Capacity Target %q",
		controller.MetaKey(capacityTarget),
	)

	return capacityTarget, nil
}

// computeTargetClusters picks out the clusters from the given list which match
// the release's clusterRequirements.
func computeTargetClusters(rel *shipper.Release, clusterList []*shipper.Cluster) ([]*shipper.Cluster, error) {
	regionSpecs := rel.Spec.Environment.ClusterRequirements.Regions
	requiredCapabilities := rel.Spec.Environment.ClusterRequirements.Capabilities
	capableClustersByRegion := map[string][]*shipper.Cluster{}
	regionReplicas := map[string]int{}

	if len(regionSpecs) == 0 {
		return nil, NewNoRegionsSpecifiedError()
	}

	app, err := releaseutil.ApplicationNameForRelease(rel)
	if err != nil {
		return nil, err
	}

	err = validateClusterRequirements(rel.Spec.Environment.ClusterRequirements)
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

	resClusters := make([]*shipper.Cluster, 0)
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
				resClusters = append(resClusters, cluster)
			}
		}
	}

	sort.Slice(resClusters, func(i, j int) bool {
		return resClusters[i].Name < resClusters[j].Name
	})

	return resClusters, nil
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

func setClusters(rel *shipper.Release, clusters []*shipper.Cluster) {
	clusterNames := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		clusterNames = append(clusterNames, cluster.Name)
	}
	sort.Strings(clusterNames)
	rel.Annotations[shipper.ReleaseClustersAnnotation] = strings.Join(clusterNames, ",")
}

func (s *Scheduler) fetchChartAndExtractReplicaCount(rel *shipper.Release) (int32, error) {
	chart, err := s.fetchChart(rel.Spec.Environment.Chart)
	if err != nil {
		return 0, NewChartFetchFailureError(
			rel.Spec.Environment.Chart.Name,
			rel.Spec.Environment.Chart.Version,
			rel.Spec.Environment.Chart.RepoURL,
			err,
		)
	}

	replicas, err := extractReplicasFromChartForRel(chart, rel)
	if err != nil {
		return 0, err
	}

	glog.V(4).Infof("Extracted %d replicas from release %q", replicas, controller.MetaKey(rel))

	return int32(replicas), nil
}

func extractReplicasFromChartForRel(chart *helmchart.Chart, rel *shipper.Release) (int32, error) {
	owners := rel.OwnerReferences
	if l := len(owners); l != 1 {
		return 0, NewInvalidReleaseOwnerRefsError(len(owners))
	}

	applicationName := owners[0].Name
	rendered, err := shipperchart.Render(chart, applicationName, rel.Namespace, rel.Spec.Environment.Values)
	if err != nil {
		return 0, NewBrokenChartError(
			rel.Spec.Environment.Chart.Name,
			rel.Spec.Environment.Chart.Version,
			rel.Spec.Environment.Chart.RepoURL,
			err,
		)
	}

	deployments := shipperchart.GetDeployments(rendered)
	if len(deployments) != 1 {
		return 0, NewWrongChartDeploymentsError(
			rel.Spec.Environment.Chart.Name,
			rel.Spec.Environment.Chart.Version,
			rel.Spec.Environment.Chart.RepoURL,
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
