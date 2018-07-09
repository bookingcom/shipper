package schedulecontroller

import (
	"fmt"
	"sort"
	"strings"

	helmchart "k8s.io/helm/pkg/proto/hapi/chart"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/record"

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

	// Compute target clusters, and update the release if it
	// doesn't have any
	if !c.HasClusters() {
		clusters, err := c.ComputeTargetClusters()
		if err == nil {
			newRelease, updateErr := c.UpdateRelease()
			if updateErr != nil {
				return updateErr
			}
			c.Release = newRelease
		}

		if err == nil {
			c.recorder.Eventf(
				c.Release,
				corev1.EventTypeNormal,
				"ClustersSelected",
				"Set clusters for %q to %v",
				controller.MetaKey(c.Release),
				clusters,
			)
		}
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

	// If we get to this point, it means that the clusters have already been selected and persisted in the Release
	// document, and all the associated Release documents have already been created, so the last operation remaining is
	// updating the PhaseStatus to ReleasePhaseWaitingForStrategy
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
		return 0, fmt.Errorf("fetch chart %v for release %q: %s",
			c.Release.Environment.Chart, controller.MetaKey(c.Release), err)
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
		return 0, fmt.Errorf("Error when rendering the chart: expected exactly 1 owner for release %s, but got %d", controller.MetaKey(c.Release), l)
	}

	applicationName := owners[0].Name
	rendered, err := shipperchart.Render(chart, applicationName, c.Release.Namespace, c.Release.Environment.Values)
	if err != nil {
		return 0, fmt.Errorf("Error while extracting replicas for release %q: %s",
			controller.MetaKey(c.Release), err)
	}

	deployments := shipperchart.GetDeployments(rendered)
	if n := len(deployments); n != 1 {
		return 0, fmt.Errorf("Error while extracting replicas for release %q: expected exactly one Deployment in the chart but got %d",
			controller.MetaKey(c.Release), n)
	}

	replicas := deployments[0].Spec.Replicas
	// deployments default to 1 replica when replicas is nil or unspecified
	// see k8s.io/api/apps/v1/types.go DeploymentSpec
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
		return err
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

// CreateCapacityTarget creates a new CapacityTarget object for
// Scheduler's Release property. Returns an error if the object couldn't
// be created, except in cases where the object already exists.
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
		return err
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

// CreateTrafficTarget creates a new TrafficTarget object for
// Scheduler's Release property. Returns an error if the object couldn't
// be created, except in cases where the object already exists.
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
		return err
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

// ComputeTargetClusters updates the Release Environment.Clusters property
// based on its ClusterRequirements.
func (c *Scheduler) ComputeTargetClusters() ([]string, error) {
	// TODO selectors should be calculated from
	// c.Release.Environment.ClusterRequirements
	selectors := labels.NewSelector()

	clusters, err := c.clustersLister.List(selectors)
	if err != nil {
		return nil, err
	}

	var clusterNames []string
	for _, v := range clusters {
		if !v.Spec.Unschedulable {
			clusterNames = append(clusterNames, v.Name)
		}
	}
	c.SetClusters(clusterNames)

	return clusterNames, nil
}

// the strings here are insane, but if you create a fresh release object for
// some reason it lands in the work queue with an empty TypeMeta. This is resolved
// if you restart the controllers, so I'm not sure what's going on.
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
