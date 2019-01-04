package release

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/chart"
	shipperclient "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

func buildRelease() *shipper.Release {
	return &shipper.Release{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "Release",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-release",
			Namespace:   shippertesting.TestNamespace,
			Annotations: map[string]string{},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Kind:       "Application",
					Name:       "test-application",
				},
			},
		},
		Spec: shipper.ReleaseSpec{
			Environment: shipper.ReleaseEnvironment{
				Chart: shipper.Chart{
					Name:    "simple",
					Version: "0.0.1",
					RepoURL: chartRepoURL,
				},
				ClusterRequirements: shipper.ClusterRequirements{
					Regions: []shipper.RegionRequirement{{Name: shippertesting.TestRegion}},
				},
			},
		},
	}
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

func buildInstallationTarget(ns string, rel *shipper.Release) *shipper.InstallationTarget {
	//TODO(olegs): clusters should be configurable
	return &shipper.InstallationTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rel.Name,
			Namespace: ns,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: rel.APIVersion,
					Kind:       rel.Kind,
					Name:       rel.Name,
					UID:        rel.UID,
				},
			},
		},
		Spec: shipper.InstallationTargetSpec{
			Clusters: []string{"minikube-a"},
		},
	}
}

func buildCapacityTarget(ns string, rel *shipper.Release) *shipper.CapacityTarget {
	return &shipper.CapacityTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rel.Name,
			Namespace: ns,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: rel.APIVersion,
					Kind:       rel.Kind,
					Name:       rel.Name,
					UID:        rel.UID,
				},
			},
		},
		Spec: shipper.CapacityTargetSpec{
			Clusters: []shipper.ClusterCapacityTarget{
				{
					Name:              "minikube-a",
					Percent:           0,
					TotalReplicaCount: 12,
				},
			},
		},
	}
}

func buildTrafficTarget(ns string, rel *shipper.Release) *shipper.TrafficTarget {
	return &shipper.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rel.Name,
			Namespace: ns,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: rel.APIVersion,
					Kind:       rel.Kind,
					Name:       rel.Name,
					UID:        rel.UID,
				},
			},
		},
		Spec: shipper.TrafficTargetSpec{
			Clusters: []shipper.ClusterTrafficTarget{
				shipper.ClusterTrafficTarget{
					Name: "minikube-a",
				},
			},
		},
	}
}

func buildExpectedActions(ns string, rel *shipper.Release) []kubetesting.Action {
	installationTarget := buildInstallationTarget(ns, rel)

	capacityTarget := buildCapacityTarget(ns, rel)

	trafficTarget := buildTrafficTarget(ns, rel)

	actions := []kubetesting.Action{
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("installationtargets"),
			ns,
			installationTarget),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("traffictargets"),
			ns,
			trafficTarget),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("capacitytargets"),
			ns,
			capacityTarget,
		),
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			ns,
			rel),
	}

	return actions
}

func newReleaseController(clientset shipperclient.Interface) *ReleaseController {
	informerFactory := shipperinformers.NewSharedInformerFactory(clientset, time.Millisecond*0)
	controller := NewReleaseController(
		clientset,
		informerFactory,
		chart.FetchRemote(),
		&fakedynamic.FakeClientPool{},
		record.NewFakeRecorder(42),
	)

	stopCh := make(chan struct{})
	defer close(stopCh)

	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)

	return controller
}
