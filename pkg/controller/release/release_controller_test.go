package release

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakediscovery "k8s.io/client-go/discovery/fake"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/tools/record"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/chart"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

var vanguard = shipper.RolloutStrategy{
	Steps: []shipper.RolloutStrategyStep{
		{
			Name:     "staging",
			Capacity: shipper.RolloutStrategyStepValue{Incumbent: 100, Contender: 1},
			Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 100, Contender: 0},
		},
		{
			Name:     "50/50",
			Capacity: shipper.RolloutStrategyStepValue{Incumbent: 50, Contender: 50},
			Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 50, Contender: 50},
		},
		{
			Name:     "full on",
			Capacity: shipper.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
			Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
		},
	},
}

type fixture struct {
	t          *testing.T
	objects    []runtime.Object
	clientset  *shipperfake.Clientset
	discovery  *fakediscovery.FakeDiscovery
	clientPool *fakedynamic.FakeClientPool
	recorder   *record.FakeRecorder
}

func newFixture(t *testing.T, objects ...runtime.Object) *fixture {
	clientset := shipperfake.NewSimpleClientset(objects...)
	discovery := clientset.Discovery().(*fakediscovery.FakeDiscovery)
	discovery.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: shipper.SchemeGroupVersion.String(),
			APIResources: []metav1.APIResource{
				{
					Kind:       "Application",
					Namespaced: true,
					Name:       "applications",
				},
				{
					Kind:       "Release",
					Namespaced: true,
					Name:       "releases",
				},
				{
					Kind:       "CapacityTarget",
					Namespaced: true,
					Name:       "capacitytargets",
				},
				{
					Kind:       "InstallationTarget",
					Namespaced: true,
					Name:       "installationtargets",
				},
				{
					Kind:       "TrafficTarget",
					Namespaced: true,
					Name:       "traffictargets",
				},
			},
		},
	}
	clientPool := &fakedynamic.FakeClientPool{}
	recorder := record.NewFakeRecorder(42)

	return &fixture{
		t:          t,
		objects:    objects,
		clientset:  clientset,
		discovery:  discovery,
		clientPool: clientPool,
		recorder:   recorder,
	}
}

func (f *fixture) newController() *Controller {
	var syncPeriod time.Duration = 0
	informerFactory := shipperinformers.NewSharedInformerFactory(f.clientset, syncPeriod)
	return NewController(
		f.clientset,
		informerFactory,
		chart.FetchRemote(),
		f.clientPool,
		f.recorder,
	)
}

func buildCluster(name string) *shipper.Cluster {
	return &shipper.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: shipper.ClusterSpec{
			APIMaster:    "https://127.0.0.1",
			Capabilities: []string{},
			Region:       shippertesting.TestRegion,
		},
	}
}

func (f *fixture) buildContender(relName string, namespace string, replicaCount int32) *releaseInfo {
	var app *shipper.Application
	for _, object := range f.objects {
		if conv, ok := object.(*shipper.Application); ok {
			app = conv
			break
		}
	}
	if app == nil {
		f.t.Fatalf("The fixture is missing an Application object")
	}

	rel := &shipper.Release{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "Release",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      relName,
			Namespace: namespace,
			OwnerReferences: []metav1.OwnerReference{
				metav1.OwnerReference{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Kind:       "Application",
					Name:       app.GetName(),
					UID:        app.GetUID(),
				},
			},
			Labels: map[string]string{
				shipper.ReleaseLabel: relName,
				shipper.AppLabel:     app.GetName(),
			},
			Annotations: map[string]string{
				shipper.ReleaseGenerationAnnotation: "1",
			},
		},
		Status: shipper.ReleaseStatus{
			Conditions: []shipper.ReleaseCondition{},
		},
		Spec: shipper.ReleaseSpec{
			TargetStep: 0,
			Environment: shipper.ReleaseEnvironment{
				Strategy: &vanguard,
			},
		},
	}

	clusterNames := make([]string, 0)
	for _, obj := range f.objects {
		if cluster, ok := obj.(*shipper.Cluster); ok {
			clusterNames = append(clusterNames, cluster.GetName())
		}
	}
	if len(clusterNames) == 0 {
		f.t.Fatalf("The fixture is missing at least 1 Cluster object")
	}

	installationTargetClusters := make([]*shipper.ClusterInstallationStatus, 0, len(clusterNames))
	for _, clusterName := range clusterNames {
		installationTargetClusters = append(installationTargetClusters, &shipper.ClusterInstallationStatus{
			Name:   clusterName,
			Status: shipper.InstallationStatusInstalled,
		})
	}

	installationTarget := &shipper.InstallationTarget{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "InstallationTarget",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      relName,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Name:       relName,
					Kind:       "Release",
					UID:        rel.GetUID(),
				},
			},
		},
		Status: shipper.InstallationTargetStatus{
			Clusters: installationTargetClusters,
		},
		Spec: shipper.InstallationTargetSpec{
			Clusters: clusterNames,
		},
	}

	capacityTargetStatusClusters := make([]shipper.ClusterCapacityStatus, len(clusterNames))
	capacityTargetSpecClusters := make([]shipper.ClusterCapacityTarget, len(clusterNames))
	for _, clusterName := range clusterNames {
		capacityTargetStatusClusters = append(capacityTargetStatusClusters, shipper.ClusterCapacityStatus{
			Name:            clusterName,
			AchievedPercent: 100,
		})
		capacityTargetSpecClusters = append(capacityTargetSpecClusters, shipper.ClusterCapacityTarget{
			Name:              clusterName,
			Percent:           0,
			TotalReplicaCount: replicaCount,
		})
	}

	capacityTarget := &shipper.CapacityTarget{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "CapacityTarget",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      relName,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Name:       rel.GetName(),
					Kind:       "Release",
					UID:        rel.GetUID(),
				},
			},
		},
		Status: shipper.CapacityTargetStatus{
			Clusters: capacityTargetStatusClusters,
		},
		Spec: shipper.CapacityTargetSpec{
			Clusters: capacityTargetSpecClusters,
		},
	}

	trafficTargetStatusClusters := make([]*shipper.ClusterTrafficStatus, len(clusterNames))
	trafficTargetSpecClusters := make([]shipper.ClusterTrafficTarget, len(clusterNames))

	for _, clusterName := range clusterNames {
		trafficTargetStatusClusters = append(trafficTargetStatusClusters, &shipper.ClusterTrafficStatus{
			Name:            clusterName,
			AchievedTraffic: 100,
		})
		trafficTargetSpecClusters = append(trafficTargetSpecClusters, shipper.ClusterTrafficTarget{
			Name:   clusterName,
			Weight: 0,
		})
	}

	trafficTarget := &shipper.TrafficTarget{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "TrafficTarget",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      relName,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Name:       rel.GetName(),
					Kind:       "Release",
					UID:        rel.GetUID(),
				},
			},
		},
		Status: shipper.TrafficTargetStatus{
			Clusters: trafficTargetStatusClusters,
		},
		Spec: shipper.TrafficTargetSpec{
			Clusters: trafficTargetSpecClusters,
		},
	}

	return &releaseInfo{
		release:            rel,
		installationTarget: installationTarget,
		capacityTarget:     capacityTarget,
		trafficTarget:      trafficTarget,
	}
}
