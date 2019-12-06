package traffic

import (
	"fmt"
	"sort"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
)

const (
	podReadinessLabel = "shipper-test-pod-ready"
	podReady          = "true"
	podNotReady       = "false"

	withTraffic = true
	noTraffic   = false
)

type podStatus struct {
	withTraffic    int
	withoutTraffic int
}

var (
	TargetConditionOperational = shipper.TargetCondition{
		Type:   shipper.TargetConditionTypeOperational,
		Status: corev1.ConditionTrue,
	}
	TargetConditionReady = shipper.TargetCondition{
		Type:   shipper.TargetConditionTypeReady,
		Status: corev1.ConditionTrue,
	}
	ClusterTrafficOperational = shipper.ClusterTrafficCondition{
		Type:   shipper.ClusterConditionTypeOperational,
		Status: corev1.ConditionTrue,
	}
	ClusterTrafficReady = shipper.ClusterTrafficCondition{
		Type:   shipper.ClusterConditionTypeReady,
		Status: corev1.ConditionTrue,
	}
)

// NOTE: this does not try to implement endpoint subsets at all. All pods are
// assumed to be part of the same subset, and it checks magic labels to decide
// if they're ready.
func shiftPodInEndpoints(pod *corev1.Pod, endpoints *corev1.Endpoints) *corev1.Endpoints {
	var podGetsTraffic bool
	trafficStatusLabel, ok := pod.Labels[shipper.PodTrafficStatusLabel]
	if ok {
		podGetsTraffic = trafficStatusLabel == shipper.Enabled
	}

	endpoints = endpoints.DeepCopy()

	var addresses []corev1.EndpointAddress
	ready := true
	readyLabel, ok := pod.Labels[podReadinessLabel]
	if ok {
		ready = readyLabel == podReady
	}

	if ready {
		addresses = endpoints.Subsets[0].Addresses
	} else {
		addresses = endpoints.Subsets[0].NotReadyAddresses
	}

	addressIndex := -1
	for i, address := range addresses {
		if address.TargetRef.Name == pod.Name {
			addressIndex = i
			break
		}
	}

	if podGetsTraffic && addressIndex == -1 {
		addresses = append(addresses, corev1.EndpointAddress{
			TargetRef: &corev1.ObjectReference{
				Kind:      "Pod",
				Namespace: pod.Namespace,
				Name:      pod.Name,
			},
		})
	} else if !podGetsTraffic && addressIndex >= 0 {
		addresses = append(addresses[:addressIndex], addresses[:addressIndex+1]...)
	}

	if ready {
		endpoints.Subsets[0].Addresses = addresses
	} else {
		endpoints.Subsets[0].NotReadyAddresses = addresses
	}

	return endpoints
}

func buildTrafficTarget(app, release string, clusterWeights map[string]uint32) *shipper.TrafficTarget {
	clusters := make([]shipper.ClusterTrafficTarget, 0, len(clusterWeights))

	for cluster, weight := range clusterWeights {
		clusters = append(clusters, shipper.ClusterTrafficTarget{
			Name:   cluster,
			Weight: weight,
		})
	}

	return &shipper.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release,
			Namespace: shippertesting.TestNamespace,
			Labels: map[string]string{
				shipper.AppLabel:     app,
				shipper.ReleaseLabel: release,
			},
		},
		Spec: shipper.TrafficTargetSpec{
			Clusters: clusters,
		},
	}
}

func buildSuccessStatus(clusters []shipper.ClusterTrafficTarget) shipper.TrafficTargetStatus {
	clusterStatuses := make([]*shipper.ClusterTrafficStatus, 0, len(clusters))

	for _, cluster := range clusters {
		clusterStatuses = append(clusterStatuses, &shipper.ClusterTrafficStatus{
			Name:            cluster.Name,
			AchievedTraffic: cluster.Weight,
			Conditions: []shipper.ClusterTrafficCondition{
				ClusterTrafficOperational,
				ClusterTrafficReady,
			},
		})
	}

	sort.Sort(byClusterName(clusterStatuses))

	return shipper.TrafficTargetStatus{
		Clusters: clusterStatuses,
		Conditions: []shipper.TargetCondition{
			TargetConditionOperational,
			TargetConditionReady,
		},
	}
}

func buildService(app string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-prod", app),
			Namespace: shippertesting.TestNamespace,
			Labels: map[string]string{
				shipper.LBLabel:  shipper.LBForProduction,
				shipper.AppLabel: app,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				shipper.AppLabel:              app,
				shipper.PodTrafficStatusLabel: shipper.Enabled,
			},
		},
	}
}

func buildEndpoints(app string) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-prod", app),
			Namespace: shippertesting.TestNamespace,
			Labels: map[string]string{
				shipper.LBLabel:  shipper.LBForProduction,
				shipper.AppLabel: app,
			},
		},
		Subsets: []corev1.EndpointSubset{
			corev1.EndpointSubset{
				Addresses: []corev1.EndpointAddress{},
			},
		},
	}
}

var podId int

func buildPods(app, release string, count int, withTraffic bool) []*corev1.Pod {
	pods := make([]*corev1.Pod, 0, count)

	for i := 0; i < count; i++ {
		getsTraffic := shipper.Enabled
		if !withTraffic {
			getsTraffic = shipper.Disabled
		}

		podId += 1
		pods = append(pods, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", release, podId),
				Namespace: shippertesting.TestNamespace,
				Labels: map[string]string{
					shipper.AppLabel:              app,
					shipper.ReleaseLabel:          release,
					shipper.PodTrafficStatusLabel: getsTraffic,
				},
			},
		})
	}

	return pods
}

func buildWorldWithPods(app, release string, n int, traffic bool) []runtime.Object {
	objects := []runtime.Object{
		buildService(app),
		buildEndpoints(app),
	}

	objects = addPodsToList(objects, buildPods(app, release, n, traffic))

	return objects
}

func addPodsToList(objects []runtime.Object, pods []*corev1.Pod) []runtime.Object {
	for _, pod := range pods {
		objects = append(objects, pod)
	}

	return objects
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
