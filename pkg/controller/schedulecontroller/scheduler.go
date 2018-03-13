package schedulecontroller

import (
	"github.com/golang/glog"
	"github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// Scheduler is an object that knows how to schedule releases.
type Scheduler struct {
	Release          *v1.Release
	shipperclientset clientset.Interface
	clustersLister   listers.ClusterLister
}

// NewScheduler returns a new Scheduler instance that knows how to
// schedule a particular Release.
func NewScheduler(
	release *v1.Release,
	shipperclientset clientset.Interface,
	clusterLister listers.ClusterLister,
) *Scheduler {
	return &Scheduler{
		Release:          release.DeepCopy(),
		shipperclientset: shipperclientset,
		clustersLister:   clusterLister,
	}
}

func (c *Scheduler) scheduleRelease() error {

	glog.Infof("Processing release %s/%s", c.Release.Namespace, c.Release.Name)

	// Compute target clusters, update the release if it doesn't have any, and bail-out. Since we haven't updated
	// .Status.Phase yet, this update will trigger a new item on the work queue.
	if !c.HasClusters() {
		if err := c.ComputeTargetClusters(); err != nil {
			return err
		}
		return c.UpdateRelease()
	}

	if err := c.CreateInstallationTarget(); err != nil {
		return err
	}

	if err := c.CreateTrafficTarget(); err != nil {
		return err
	}

	if err := c.CreateCapacityTarget(); err != nil {
		return err
	}

	// If we get to this point, it means that the clusters have already been selected and persisted in the Release
	// document, and all the associated Release documents have already been created, so the last operation remaining is
	// updating the PhaseStatus to ReleasePhaseWaitingForStrategy
	defer glog.Infof("Finished processing %s/%s", c.Release.Namespace, c.Release.Name)
	c.SetReleasePhase(v1.ReleasePhaseWaitingForStrategy)
	return c.UpdateRelease()
}

func (c *Scheduler) HasClusters() bool {
	return len(c.Release.Environment.Clusters) > 0
}

func (c *Scheduler) Clusters() []string {
	return c.Release.Environment.Clusters
}

func (c *Scheduler) SetClusters(clusters []string) {
	c.Release.Environment.Clusters = clusters
}

func (c *Scheduler) SetReleasePhase(phase string) {
	c.Release.Status.Phase = phase
}

func (c *Scheduler) UpdateRelease() error {
	_, err := c.shipperclientset.ShipperV1().Releases(c.Release.Namespace).Update(c.Release)
	return err
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

	if created, err := c.shipperclientset.ShipperV1().InstallationTargets(c.Release.Namespace).Create(installationTarget); err != nil {
		if errors.IsAlreadyExists(err) {
			glog.Infof("InstallationTarget %s/%s already exists, moving on", c.Release.Namespace, c.Release.Name)
			return nil
		}
		glog.Error(err)
		return err
	} else {
		glog.Infof("InstallationTarget %s/%s created", created.Namespace, created.Name)
		return nil
	}
}

// CreateCapacityTarget creates a new CapacityTarget object for
// Scheduler's Release property. Returns an error if the object couldn't
// be created, except in cases where the object already exists.
func (c *Scheduler) CreateCapacityTarget() error {
	count := len(c.Clusters())
	targets := make([]v1.ClusterCapacityTarget, count)
	for i, v := range c.Clusters() {
		targets[i] = v1.ClusterCapacityTarget{Name: v, Percent: 0}
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
		Spec: v1.CapacityTargetSpec{Clusters: targets},
	}

	if created, err := c.shipperclientset.ShipperV1().CapacityTargets(c.Release.Namespace).Create(capacityTarget); err != nil {
		if errors.IsAlreadyExists(err) {
			glog.Infof("CapacityTarget %s/%s already exists, moving on", c.Release.Namespace, c.Release.Name)
			return nil
		}
		glog.Error(err)
		return err
	} else {
		glog.Infof("CapacityTarget %s/%s has been created", created.Namespace, created.Name)
		return nil
	}
}

// CreateTrafficTarget creates a new TrafficTarget object for
// Scheduler's Release property. Returns an error if the object couldn't
// be created, except in cases where the object already exists.
func (c *Scheduler) CreateTrafficTarget() error {
	count := len(c.Clusters())
	trafficTargets := make([]v1.ClusterTrafficTarget, count)
	for i, v := range c.Clusters() {
		trafficTargets[i] = v1.ClusterTrafficTarget{Name: v, TargetTraffic: 0}
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

	if created, err := c.shipperclientset.ShipperV1().TrafficTargets(c.Release.Namespace).Create(trafficTarget); err != nil {
		if errors.IsAlreadyExists(err) {
			glog.Infof("TrafficTarget %s/%s already exists, moving on", c.Release.Namespace, c.Release.Name)
			return nil
		}
		glog.Error(err)
		return err
	} else {
		glog.Infof("TrafficTarget %s/%s created", created.Namespace, created.Name)
		return nil
	}
}

// ComputeTargetClusters updates the Release Environment.Clusters property
// based on its ClusterSelectors.
func (c *Scheduler) ComputeTargetClusters() error {

	// selectors should be calculated from c.Release.Environment.ShipmentOrder.ClusterSelectors
	selectors := labels.NewSelector()

	if clusters, err := c.clustersLister.List(selectors); err != nil {
		return err
	} else {
		clusterNames := make([]string, 0)
		for _, v := range clusters {
			if !v.Spec.Unschedulable {
				clusterNames = append(clusterNames, v.Name)
			}
		}

		c.SetClusters(clusterNames)
		return nil
	}
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
