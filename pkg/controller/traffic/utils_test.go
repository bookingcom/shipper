package traffic

import (
	"fmt"
	"reflect"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	"github.com/pmezard/go-difflib/difflib"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
)

// NOTE: this does not try to implement endpoint subsets at all. All
// pods are assumed to be part of the same subset and to be always ready.
func shiftPodInEndpoints(pod *corev1.Pod, endpoints *corev1.Endpoints) *corev1.Endpoints {
	var podGetsTraffic bool
	trafficStatusLabel, ok := pod.Labels[shipper.PodTrafficStatusLabel]
	if ok {
		podGetsTraffic = trafficStatusLabel == shipper.Enabled
	}

	addresses := endpoints.Subsets[0].Addresses
	addressIndex := -1
	for i, address := range addresses {
		if address.TargetRef.Name == pod.Name {
			addressIndex = i
			break
		}
	}

	if podGetsTraffic && addressIndex == -1 {
		address := corev1.EndpointAddress{
			TargetRef: &corev1.ObjectReference{
				Kind:      "Pod",
				Namespace: pod.Namespace,
				Name:      pod.Name,
			},
		}

		endpoints.Subsets[0] = corev1.EndpointSubset{
			Addresses: append(addresses, address),
		}
	} else if !podGetsTraffic && addressIndex >= 0 {
		endpoints.Subsets[0] = corev1.EndpointSubset{
			Addresses: append(addresses[:addressIndex], addresses[:addressIndex+1]...),
		}
	}

	return endpoints
}

func yamlDiff(a interface{}, b interface{}) (string, error) {
	yamlActual, _ := yaml.Marshal(a)
	yamlExpected, _ := yaml.Marshal(b)

	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(yamlExpected)),
		B:        difflib.SplitLines(string(yamlActual)),
		FromFile: "Expected",
		ToFile:   "Actual",
		Context:  4,
	}

	return difflib.GetUnifiedDiffString(diff)
}

func deepEqualDiff(expected, actual interface{}) (bool, string) {
	if !reflect.DeepEqual(actual, expected) {
		diff, err := yamlDiff(actual, expected)
		if err != nil {
			panic(fmt.Sprintf("couldn't generate yaml diff: %s", err))
		}

		return false, diff
	}

	return true, ""
}

type ControllerTestFixture struct {
	ShipperClient          *shipperfake.Clientset
	ShipperInformerFactory shipperinformers.SharedInformerFactory

	Clusters           map[string]*shippertesting.FakeCluster
	ClusterClientStore *shippertesting.FakeClusterClientStore

	Recorder *record.FakeRecorder
}

func NewControllerTestFixture() *ControllerTestFixture {
	const recorderBufSize = 42

	shipperClient := shipperfake.NewSimpleClientset()
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(
		shipperClient, shippertesting.NoResyncPeriod)

	store := shippertesting.NewFakeClusterClientStore(map[string]*shippertesting.FakeCluster{})

	return &ControllerTestFixture{
		ShipperClient:          shipperClient,
		ShipperInformerFactory: shipperInformerFactory,

		Clusters:           make(map[string]*shippertesting.FakeCluster),
		ClusterClientStore: store,

		Recorder: record.NewFakeRecorder(recorderBufSize),
	}
}

func (f *ControllerTestFixture) AddNamedCluster(name string) *shippertesting.FakeCluster {
	cluster := shippertesting.NewNamedFakeCluster(
		name,
		kubefake.NewSimpleClientset(), nil)

	f.Clusters[cluster.Name] = cluster
	f.ClusterClientStore.AddCluster(cluster)

	return cluster
}

func (f *ControllerTestFixture) AddCluster() *shippertesting.FakeCluster {
	name := fmt.Sprintf("cluster-%d", len(f.Clusters))
	return f.AddNamedCluster(name)
}

func (f *ControllerTestFixture) Run(stopCh chan struct{}) {
	f.ShipperInformerFactory.Start(stopCh)
	f.ShipperInformerFactory.WaitForCacheSync(stopCh)
	f.ClusterClientStore.Run(stopCh)
}
