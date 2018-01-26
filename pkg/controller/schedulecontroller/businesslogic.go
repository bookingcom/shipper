package schedulecontroller

import (
	"github.com/golang/glog"
	"github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	PhaseLabel     = "phase"
	ReleaseLabel   = "release"
	ReleaseLinkAnn = "releaseLink"

	WaitingForSchedulingPhase = "WaitingForScheduling"
	WaitingForStrategyPhase   = "WaitingForStrategy"
)

func (c *Controller) businessLogic(release *v1.Release) error {

	glog.Infof("Processing release %s/%s", release.Namespace, release.Name)

	var clusterNames []string

	// Compute target clusters, update the release if it doesn't have any, and bail-out. Since we haven't updated
	// PhaseLabel yet, this update will trigger a new item on the work queue.
	if len(release.Environment.Clusters) == 0 {
		clusterNames, err := c.computeTargetClusters(release.Environment.ShipmentOrder.ClusterSelectors)
		if err != nil {
			glog.Error(err)
			return err
		}

		release.Environment.Clusters = clusterNames
		_, err = c.shipperclientset.ShipperV1().Releases(release.Namespace).Update(release)
		return err
	} else {
		glog.Infof("Found clusters in Release %s/%s, moving on", release.Namespace, release.Name)
		clusterNames = release.Environment.Clusters
	}

	installationTarget, err := c.shipperclientset.
		ShipperV1().
		InstallationTargets(release.Namespace).
		Create(NewInstallationTarget(release, clusterNames))
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			glog.Error(err)
			return err
		} else {
			glog.Infof("InstallationTarget %s/%s already exists, moving on", release.Namespace, release.Name)
		}
	} else {
		glog.Infof("InstallationTarget %s/%s created", installationTarget.Namespace, installationTarget.Name)
	}

	trafficTarget, err := c.shipperclientset.
		ShipperV1().
		TrafficTargets(release.Namespace).
		Create(NewTrafficTarget(release, clusterNames))
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			glog.Error(err)
			return err
		} else {
			glog.Infof("TrafficTarget %s/%s already exists, moving on", release.Namespace, release.Name)
		}
	} else {
		glog.Infof("TrafficTarget %s/%s created", trafficTarget.Namespace, trafficTarget.Name)
	}

	capacityTarget, err := c.shipperclientset.
		ShipperV1().
		CapacityTargets(release.Namespace).
		Create(NewCapacityTarget(release, clusterNames))
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			glog.Error(err)
			return err
		} else {
			glog.Infof("CapacityTarget %s/%s already exists, moving on", release.Namespace, release.Name)
		}
	} else {
		glog.Infof("CapacityTarget %s/%s has been created", capacityTarget.Namespace, capacityTarget.Name)
	}

	// If we get to this point, it means that the clusters have already been selected and persisted in the Release
	// document, and all the associated Release documents have already been created, so the last operation remaining is
	// updating the PhaseLabel to WaitingForStrategyPhase.
	release.Labels[PhaseLabel] = WaitingForStrategyPhase
	_, err = c.shipperclientset.ShipperV1().Releases(release.Namespace).Update(release)
	if err != nil {
		return err
	}

	glog.Infof("Finished processing %s/%s", release.Namespace, release.Name)

	return nil
}

func NewCapacityTarget(
	release *v1.Release,
	clusterNames []string,
) *v1.CapacityTarget {

	count := len(clusterNames)
	targets := make([]v1.ClusterCapacityTarget, count)
	for i, v := range clusterNames {
		targets[i] = v1.ClusterCapacityTarget{Name: v, Replicas: 0}
	}
	capacityTarget := &v1.CapacityTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.Name,
			Namespace: release.Namespace,
			Labels: map[string]string{
				ReleaseLabel: string(release.UID),
			},
			Annotations: map[string]string{
				ReleaseLinkAnn: release.SelfLink,
			},
		},
		Spec: v1.CapacityTargetSpec{Clusters: targets},
	}
	return capacityTarget
}

func NewTrafficTarget(
	release *v1.Release,
	clusterNames []string,
) *v1.TrafficTarget {

	count := len(clusterNames)
	trafficTargets := make([]v1.ClusterTrafficTarget, count)
	for i, v := range clusterNames {
		trafficTargets[i] = v1.ClusterTrafficTarget{Name: v, TargetTraffic: 0}
	}

	trafficTarget := &v1.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.Name,
			Namespace: release.Namespace,
			Labels: map[string]string{
				ReleaseLabel: string(release.UID),
			},
			Annotations: map[string]string{
				ReleaseLinkAnn: release.SelfLink,
			},
		},
		Spec: v1.TrafficTargetSpec{Clusters: trafficTargets},
	}
	return trafficTarget
}

func NewInstallationTarget(
	release *v1.Release,
	clusterNames []string,
) *v1.InstallationTarget {

	installationTarget := &v1.InstallationTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.Name,
			Namespace: release.Namespace,
			Labels: map[string]string{
				ReleaseLabel: string(release.UID),
			},
			Annotations: map[string]string{
				ReleaseLinkAnn: release.SelfLink,
			},
		},
		Spec: v1.InstallationTargetSpec{Clusters: clusterNames},
	}
	return installationTarget
}

//noinspection GoUnusedParameter
func (c *Controller) computeTargetClusters(selectors []v1.ClusterSelector) ([]string, error) {

	// TODO: Add cluster label selector (only schedule-able clusters, for example)
	clusters, err := c.clustersLister.List(labels.NewSelector())
	if err != nil {
		return nil, err
	}

	count := len(clusters)
	names := make([]string, count)
	for i, v := range clusters {
		names[i] = v.Name
	}

	return names, nil
}
