package installation

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"time"

	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"github.com/bookingcom/shipper/pkg/chart"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	"k8s.io/apimachinery/pkg/types"
)

var chartFetchFunc = chart.FetchRemoteWithCache("testdata/chart-cache", chart.DefaultCacheLimit)

// FakeClientProvider implements clusterclientstore.ClientProvider
type FakeClientProvider struct {
	fakeClient          kubernetes.Interface
	restConfig          *rest.Config
	getClientShouldFail bool
	getConfigShouldFail bool
}

func (f *FakeClientProvider) GetClient(clusterName string) (kubernetes.Interface, error) {
	if f.getClientShouldFail {
		return nil, fmt.Errorf("client error")
	} else {
		return f.fakeClient, nil
	}
}

func (f *FakeClientProvider) GetConfig(clusterName string) (*rest.Config, error) {
	if f.getConfigShouldFail {
		return nil, fmt.Errorf("config error")
	} else {
		return f.restConfig, nil
	}
}

func loadService(variant string) *coreV1.Service {
	service := &coreV1.Service{}
	serviceYamlPath := filepath.Join("testdata", fmt.Sprintf("service-%s.yaml", variant))

	if serviceRaw, err := ioutil.ReadFile(serviceYamlPath); err != nil {
		panic(err)
	} else if _, _, err = scheme.Codecs.UniversalDeserializer().Decode(serviceRaw, nil, service); err != nil {
		panic(err)
	}

	return service
}

func buildApplication(appName, ns string) *shipperV1.Application {
	return &shipperV1.Application{
		ObjectMeta: v1.ObjectMeta{
			Name:      appName,
			Namespace: ns,
		},
		Status: shipperV1.ApplicationStatus{
			History: []string{"0.0.1"},
		},
		Spec: shipperV1.ApplicationSpec{
			Template: shipperV1.ReleaseEnvironment{
				ClusterRequirements: shipperV1.ClusterRequirements{
					Regions: []shipperV1.RegionRequirement{{Name: "local"}},
				},
				Chart: shipperV1.Chart{
					Name:    "nginx",
					Version: "0.1.0",
					RepoURL: "https://chartmuseum.local/charts",
				},
				Values: &shipperV1.ChartValues{
					"replicaCount": "10",
				},
				Strategy: &shipperV1.RolloutStrategy{
					Steps: []shipperV1.RolloutStrategyStep{
						{
							Name: "staging",
							Capacity: shipperV1.RolloutStrategyStepValue{
								Contender: 1,
								Incumbent: 100,
							},
							Traffic: shipperV1.RolloutStrategyStepValue{
								Contender: 0,
								Incumbent: 100,
							},
						},
						{
							Name: "50/50",
							Capacity: shipperV1.RolloutStrategyStepValue{
								Contender: 50,
								Incumbent: 50,
							},
							Traffic: shipperV1.RolloutStrategyStepValue{
								Contender: 50,
								Incumbent: 50,
							},
						},
						{
							Name: "full on",
							Capacity: shipperV1.RolloutStrategyStepValue{
								Contender: 100,
								Incumbent: 0,
							},
							Traffic: shipperV1.RolloutStrategyStepValue{
								Contender: 100,
								Incumbent: 0,
							},
						},
					},
				},
			},
		},
	}
}

// buildCluster returns a cluster.
func buildCluster(name string) *shipperV1.Cluster {
	return &shipperV1.Cluster{
		ObjectMeta: v1.ObjectMeta{
			Name: name,
		},
		Status: shipperV1.ClusterStatus{
			InService: true,
		},
	}
}

// populateFakeDiscovery adds the apiResourceList into the given fake
// discovery.
func populateFakeDiscovery(discovery discovery.DiscoveryInterface, apiResourceList []*v1.APIResourceList) {
	fakeDiscovery := discovery.(*fakediscovery.FakeDiscovery)
	fakeDiscovery.Resources = apiResourceList
}

// initializeClients returns some objects that are used in several
// tests, basically to reduce boilerplate.
func initializeClients(apiResourceList []*v1.APIResourceList, shipperObjects []runtime.Object, kubeObjects []runtime.Object) (
	kubernetes.Interface,
	*shipperfake.Clientset,
	*dynamicfake.FakeClient,
	DynamicClientBuilderFunc,
	shipperinformers.SharedInformerFactory,
) {
	fakeClient := kubefake.NewSimpleClientset(kubeObjects...)
	populateFakeDiscovery(fakeClient.Discovery(), apiResourceList)
	fakeDynamicClient := &dynamicfake.FakeClient{
		Fake: &kubetesting.Fake{},
	}
	fakeDynamicClientBuilder := func(kind *schema.GroupVersionKind, restConfig *rest.Config) dynamic.Interface {
		fakeDynamicClient.GroupVersion = kind.GroupVersion()
		return fakeDynamicClient
	}

	shipperclientset := shipperfake.NewSimpleClientset(shipperObjects...)
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(shipperclientset, time.Second*0)

	return fakeClient, shipperclientset, fakeDynamicClient, fakeDynamicClientBuilder, shipperInformerFactory
}

// newController returns an installation.Controller after it has started and
// waited for informer caches sync and there is something on the controller's
// workqueue.
func newController(
	shipperclientset *shipperfake.Clientset,
	shipperInformerFactory shipperinformers.SharedInformerFactory,
	fakeClientProvider clusterclientstore.ClientProvider,
	fakeDynamicClientBuilder DynamicClientBuilderFunc,
	fakeRecorder record.EventRecorder,
) *Controller {
	c := NewController(
		shipperclientset, shipperInformerFactory, fakeClientProvider, fakeDynamicClientBuilder, chartFetchFunc,
		fakeRecorder,
	)

	stopCh := make(chan struct{})
	defer close(stopCh)

	shipperInformerFactory.Start(stopCh)
	shipperInformerFactory.WaitForCacheSync(stopCh)

	wait.PollUntil(
		10*time.Millisecond,
		func() (bool, error) { return c.workqueue.Len() >= 1, nil },
		stopCh,
	)

	return c
}

func newInstaller(release *shipperV1.Release, it *shipperV1.InstallationTarget) *Installer {
	return NewInstaller(chartFetchFunc, release, it)
}

func buildRelease(name, namespace, generation, uid, appName string) *shipperV1.Release {
	return &shipperV1.Release{
		ReleaseMeta: shipperV1.ReleaseMeta{
			ObjectMeta: v1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(uid),
				Labels: map[string]string{
					shipperV1.AppLabel:     appName,
					shipperV1.ReleaseLabel: name,
				},
				Annotations: map[string]string{
					shipperV1.ReleaseGenerationAnnotation: generation,
				},
			},
			Environment: shipperV1.ReleaseEnvironment{
				Chart: shipperV1.Chart{
					Name:    "reviews-api",
					Version: "0.0.1",
					RepoURL: "localhost",
				},
			},
		},
	}
}

func buildInstallationTargetWithOwner(ownerName, ownerUID, namespace, appName string, clusters []string) *shipperV1.InstallationTarget {
	return &shipperV1.InstallationTarget{
		ObjectMeta: v1.ObjectMeta{
			Name:      ownerName,
			Namespace: namespace,
			OwnerReferences: []v1.OwnerReference{
				{
					APIVersion: "shipper.booking.com/v1",
					Kind:       "Release",
					Name:       ownerName,
					UID:        types.UID(ownerUID),
				},
			},
			Labels: map[string]string{
				shipperV1.AppLabel:     appName,
				shipperV1.ReleaseLabel: ownerName,
			},
		},
		Spec: shipperV1.InstallationTargetSpec{
			Clusters: clusters,
		},
	}
}

func buildInstallationTarget(owner *shipperV1.Release, namespace, appName string, clusters []string) *shipperV1.InstallationTarget {
	return buildInstallationTargetWithOwner(owner.Name, string(owner.UID), namespace, appName, clusters)
}
