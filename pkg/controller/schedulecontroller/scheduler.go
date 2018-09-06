package schedulecontroller

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

	"github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperchart "github.com/bookingcom/shipper/pkg/chart"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1"
	"github.com/bookingcom/shipper/pkg/controller"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

// Scheduler is an object that knows how to schedule releases.
type Scheduler struct {
	Release          *v1.Release
	shipperclientset clientset.Interface
	clustersLister   listers.ClusterLister
	fetchChart       shipperchart.FetchFunc

	recorder record.EventRecorder
}

// NewScheduler returns a new Scheduler instance that knows how to
// schedule a particular Release.
func NewScheduler(
	release *v1.Release,
	shipperclientset clientset.Interface,
	clusterLister listers.ClusterLister,
	chartFetchFunc shipperchart.FetchFunc,
	recorder record.EventRecorder,
) *Scheduler {
	return &Scheduler{
		Release:          release.DeepCopy(),
		shipperclientset: shipperclientset,
		clustersLister:   clusterLister,
		fetchChart:       chartFetchFunc,
		recorder:         recorder,
	}
}

func (c *Scheduler) scheduleRelease() error {
	glog.Infof("Processing release %q", controller.MetaKey(c.Release))
	defer glog.Infof("Finished processing %q", controller.MetaKey(c.Release))

	// Compute target clusters, and update the release if it doesn't have any.
	if !c.HasClusters() {
		clusterList, err := c.clustersLister.List(labels.Everything())
		if err != nil {
			return NewFailedAPICallError("ListClusters", err)
		}

		clusters, err := computeTargetClusters(c.Release, clusterList)
		if err != nil {
			return err
		}

		c.SetClusters(clusters)
		newRelease, err := c.UpdateRelease()
		if err != nil {
			return NewFailedAPICallError("UpdateRelease", err)
		}
		c.Release = newRelease

		c.recorder.Eventf(
			c.Release,
			corev1.EventTypeNormal,
			"ClustersSelected",
			"Set clusters for %q to %v",
			controller.MetaKey(c.Release),
			clusters,
		)
	}

	replicaCount, err := c.fetchChartAndExtractReplicaCount()
	if err != nil {
		return err
	}

	if err := c.CreateInstallationTarget(); err != nil {
		return err
	}

	if err := c.CreateTrafficTarget(); err != nil {
		return err
	}

	if err := c.CreateCapacityTarget(replicaCount); err != nil {
		return err
	}

	// If we get to this point, it means that the clusters have already been
	// selected and persisted in the Release, and all the associated Releases have
	// already been created.
	condition := releaseutil.NewReleaseCondition(v1.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&c.Release.Status, *condition)

	if len(c.Release.Status.Conditions) == 0 {
		glog.Errorf(
			"Conditions don't seem right here for Release %q",
			controller.MetaKey(c.Release))
	}

	_, err = c.UpdateRelease()
	return err
}

func (c *Scheduler) HasClusters() bool {
	return len(c.Release.Annotations[v1.ReleaseClustersAnnotation]) > 0
}

func (c *Scheduler) Clusters() []string {
	clusters := strings.Split(c.Release.Annotations[v1.ReleaseClustersAnnotation], ",")
	if len(clusters) == 1 && clusters[0] == "" {
		clusters = []string{}
	}

	return clusters
}

func (c *Scheduler) SetClusters(clusters []string) {
	sort.Strings(clusters)
	c.Release.Annotations[v1.ReleaseClustersAnnotation] = strings.Join(clusters, ",")
}

func (c *Scheduler) UpdateRelease() (*v1.Release, error) {
	return c.shipperclientset.ShipperV1().Releases(c.Release.Namespace).Update(c.Release)
}

func (c *Scheduler) fetchChartAndExtractReplicaCount() (int32, error) {
	chart, err := c.fetchChart(c.Release.Environment.Chart)
	if err != nil {
		return 0, NewChartFetchFailureError(
			c.Release.Environment.Chart.Name,
			c.Release.Environment.Chart.Version,
			c.Release.Environment.Chart.RepoURL,
			err,
		)
	}

	replicas, err := c.extractReplicasFromChart(chart)
	if err != nil {
		return 0, err
	}

	glog.V(4).Infof("Extracted %v replicas from release %q", replicas,
		controller.MetaKey(c.Release))

	return int32(replicas), nil
}

func (c *Scheduler) extractReplicasFromChart(chart *helmchart.Chart) (int32, error) {
	owners := c.Release.OwnerReferences
	if l := len(owners); l != 1 {
		return 0, NewInvalidReleaseOwnerRefsError(len(owners))
	}

	applicationName := owners[0].Name
	rendered, err := shipperchart.Render(chart, applicationName, c.Release.Namespace, c.Release.Environment.Values)
	if err != nil {
		return 0, NewBrokenChartError(
			c.Release.Environment.Chart.Name,
			c.Release.Environment.Chart.Version,
			c.Release.Environment.Chart.RepoURL,
			err,
		)
	}

	deployments := shipperchart.GetDeployments(rendered)
	if len(deployments) != 1 {
		return 0, NewWrongChartDeploymentsError(
			c.Release.Environment.Chart.Name,
			c.Release.Environment.Chart.Version,
			c.Release.Environment.Chart.RepoURL,
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
// be created, except in cases where the object already exists.
func (c *Scheduler) CreateInstallationTarget() error {
	installationTarget := &v1.InstallationTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.Release.Name,
			Namespace: c.Release.Namespace,
			Labels:    c.Release.Labels,
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromRelease(c.Release),
			},
		},
		Spec: v1.InstallationTargetSpec{Clusters: c.Clusters()},
	}

	_, err := c.shipperclientset.ShipperV1().InstallationTargets(c.Release.Namespace).Create(installationTarget)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			glog.Infof("InstallationTarget %q already exists, moving on", controller.MetaKey(c.Release))
			return nil
		}
		return NewFailedAPICallError("CreateInstallationTarget", err)
	}

	c.recorder.Eventf(
		c.Release,
		corev1.EventTypeNormal,
		"ReleaseScheduled",
		"Created InstallationTarget %q",
		controller.MetaKey(installationTarget),
	)

	return nil
}

// CreateCapacityTarget creates a new CapacityTarget object for Scheduler's
// Release property. Returns an error if the object couldn't be created, except
// in cases where the object already exists.
func (c *Scheduler) CreateCapacityTarget(totalReplicaCount int32) error {
	count := len(c.Clusters())
	targets := make([]v1.ClusterCapacityTarget, count)
	for i, v := range c.Clusters() {
		targets[i] = v1.ClusterCapacityTarget{Name: v, Percent: 0, TotalReplicaCount: totalReplicaCount}
	}
	capacityTarget := &v1.CapacityTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.Release.Name,
			Namespace: c.Release.Namespace,
			Labels:    c.Release.Labels,
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromRelease(c.Release),
			},
		},
		Spec: v1.CapacityTargetSpec{
			Clusters: targets,
		},
	}

	_, err := c.shipperclientset.ShipperV1().CapacityTargets(c.Release.Namespace).Create(capacityTarget)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			glog.Infof("CapacityTarget %q already exists, moving on", controller.MetaKey(capacityTarget))
			return nil
		}
		return NewFailedAPICallError("CreateCapacityTarget", err)
	}

	c.recorder.Eventf(
		c.Release,
		corev1.EventTypeNormal,
		"ReleaseScheduled",
		"Created CapacityTarget %q",
		controller.MetaKey(capacityTarget),
	)

	return nil
}

// CreateTrafficTarget creates a new TrafficTarget object for Scheduler's
// Release property. Returns an error if the object couldn't be created, except
// in cases where the object already exists.
func (c *Scheduler) CreateTrafficTarget() error {
	count := len(c.Clusters())
	trafficTargets := make([]v1.ClusterTrafficTarget, count)
	for i, v := range c.Clusters() {
		trafficTargets[i] = v1.ClusterTrafficTarget{Name: v, Weight: 0}
	}

	trafficTarget := &v1.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.Release.Name,
			Namespace: c.Release.Namespace,
			Labels:    c.Release.Labels,
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromRelease(c.Release),
			},
		},
		Spec: v1.TrafficTargetSpec{Clusters: trafficTargets},
	}

	_, err := c.shipperclientset.ShipperV1().TrafficTargets(c.Release.Namespace).Create(trafficTarget)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			glog.V(4).Infof("TrafficTarget %q already exists, moving on", controller.MetaKey(trafficTarget))
			return nil
		}
		return NewFailedAPICallError("CreateTrafficTarget", err)
	}

	c.recorder.Eventf(
		c.Release,
		corev1.EventTypeNormal,
		"ReleaseScheduled",
		"Created TrafficTarget %q",
		controller.MetaKey(trafficTarget),
	)

	return nil
}

// computeTargetClusters picks out the clusters from the given list which match
// the release's clusterRequirements.
func computeTargetClusters(release *v1.Release, clusterList []*v1.Cluster) ([]string, error) {
	regionSpecs := release.Environment.ClusterRequirements.Regions
	requiredCapabilities := release.Environment.ClusterRequirements.Capabilities
	capableClustersByRegion := map[string][]*v1.Cluster{}
	regionReplicas := map[string]int{}

	if len(regionSpecs) == 0 {
		return nil, NewNoRegionsSpecifiedError()
	}

	app, err := releaseutil.ApplicationNameForRelease(release)
	if err != nil {
		return nil, err
	}

	err = validateClusterRequirements(release.Environment.ClusterRequirements)
	if err != nil {
		return nil, err
	}

	prefList := buildPrefList(app, clusterList)
	// This algo could probably build up hashes instead of doing linear searches,
	// but these data sets are so tiny (1-20 items) that it'd only be useful for
	// readability.
	for _, region := range regionSpecs {
		capableClustersByRegion[region.Name] = []*v1.Cluster{}
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

func validateClusterRequirements(requirements v1.ClusterRequirements) error {
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
func createOwnerRefFromRelease(r *v1.Release) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: "shipper.booking.com/v1",
		Kind:       "Release",
		Name:       r.GetName(),
		UID:        r.GetUID(),
	}
}
