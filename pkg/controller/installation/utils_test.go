package installation

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/dynamic"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/chart"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

var chartFetchFunc = chart.FetchRemoteWithCache("testdata/chart-cache", chart.DefaultCacheLimit)

// FakeClientProvider implements clusterclientstore.ClientProvider.
type FakeClientProvider struct {
	clientsPerCluster   clientsPerClusterMap
	restConfig          *rest.Config
	getClientShouldFail bool
	getConfigShouldFail bool
}

func (f *FakeClientProvider) GetClient(clusterName string, ua string) (kubernetes.Interface, error) {
	if f.getClientShouldFail {
		return nil, fmt.Errorf("client error")
	} else {
		fakePair := f.clientsPerCluster[clusterName]
		return fakePair.fakeClient, nil
	}
}

func (f *FakeClientProvider) GetConfig(clusterName string) (*rest.Config, error) {
	if f.getConfigShouldFail {
		return nil, fmt.Errorf("config error")
	} else {
		return f.restConfig, nil
	}
}

func loadService(variant string) *corev1.Service {
	service := &corev1.Service{}
	serviceYamlPath := filepath.Join("testdata", fmt.Sprintf("service-%s.yaml", variant))

	if serviceRaw, err := ioutil.ReadFile(serviceYamlPath); err != nil {
		panic(err)
	} else if _, _, err = scheme.Codecs.UniversalDeserializer().Decode(serviceRaw, nil, service); err != nil {
		panic(err)
	}

	return service
}

func buildApplication(appName, ns string) *shipper.Application {
	return &shipper.Application{
		ObjectMeta: v1.ObjectMeta{
			Name:      appName,
			Namespace: ns,
		},
		Status: shipper.ApplicationStatus{
			History: []string{"0.0.1"},
		},
		Spec: shipper.ApplicationSpec{
			Template: shipper.ReleaseEnvironment{
				ClusterRequirements: shipper.ClusterRequirements{
					Regions: []shipper.RegionRequirement{{Name: shippertesting.TestRegion}},
				},
				Chart: shipper.Chart{
					Name:    "nginx",
					Version: "0.1.0",
					RepoURL: "https://chartmuseum.local/charts",
				},
				Values: &shipper.ChartValues{
					"replicaCount": "10",
				},
				Strategy: &shipper.RolloutStrategy{
					Steps: []shipper.RolloutStrategyStep{
						{
							Name: "staging",
							Capacity: shipper.RolloutStrategyStepValue{
								Contender: 1,
								Incumbent: 100,
							},
							Traffic: shipper.RolloutStrategyStepValue{
								Contender: 0,
								Incumbent: 100,
							},
						},
						{
							Name: "50/50",
							Capacity: shipper.RolloutStrategyStepValue{
								Contender: 50,
								Incumbent: 50,
							},
							Traffic: shipper.RolloutStrategyStepValue{
								Contender: 50,
								Incumbent: 50,
							},
						},
						{
							Name: "full on",
							Capacity: shipper.RolloutStrategyStepValue{
								Contender: 100,
								Incumbent: 0,
							},
							Traffic: shipper.RolloutStrategyStepValue{
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
func buildCluster(name string) *shipper.Cluster {
	return &shipper.Cluster{
		ObjectMeta: v1.ObjectMeta{
			Name: name,
		},
		Status: shipper.ClusterStatus{
			InService: true,
		},
	}
}

// populateFakeDiscovery adds the apiResourceList into the given fake discovery.
func populateFakeDiscovery(discovery discovery.DiscoveryInterface, apiResourceList []*v1.APIResourceList) {
	fakeDiscovery := discovery.(*fakediscovery.FakeDiscovery)
	fakeDiscovery.Resources = apiResourceList
}

type objectsPerClusterMap map[string][]runtime.Object
type fakePair struct {
	fakeClient        kubernetes.Interface
	fakeDynamicClient *fakedynamic.FakeClient
}
type clientsPerClusterMap map[string]fakePair

// initializeClients returns some objects that are used in several tests,
// basically to reduce boilerplate.
func initializeClients(apiResourceList []*v1.APIResourceList, shipperObjects []runtime.Object, kubeObjectsPerCluster objectsPerClusterMap) (
	clientsPerClusterMap,
	*shipperfake.Clientset,
	DynamicClientBuilderFunc,
	shipperinformers.SharedInformerFactory,
) {
	clientsPerCluster := make(clientsPerClusterMap)

	for clusterName, objs := range kubeObjectsPerCluster {
		fakeClient := kubefake.NewSimpleClientset(objs...)
		populateFakeDiscovery(fakeClient.Discovery(), apiResourceList)
		fakeDynamicClient := &fakedynamic.FakeClient{
			Fake: &fakeClient.Fake,
		}
		clientsPerCluster[clusterName] = fakePair{fakeClient: fakeClient, fakeDynamicClient: fakeDynamicClient}
	}

	fakeDynamicClientBuilder := func(kind *schema.GroupVersionKind, restConfig *rest.Config, cluster *shipper.Cluster) dynamic.Interface {
		if fdc, ok := clientsPerCluster[cluster.Name]; ok {
			fdc.fakeDynamicClient.GroupVersion = kind.GroupVersion()
			return fdc.fakeDynamicClient
		}
		panic(fmt.Sprintf(`couldn't find client for %q`, cluster.Name))
	}

	shipperclientset := shipperfake.NewSimpleClientset(shipperObjects...)
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(shipperclientset, time.Second*0)

	return clientsPerCluster, shipperclientset, fakeDynamicClientBuilder, shipperInformerFactory
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

func newInstaller(release *shipper.Release, it *shipper.InstallationTarget) *Installer {
	return NewInstaller(chartFetchFunc, release, it)
}

func buildRelease(name, namespace, generation, uid, appName string) *shipper.Release {
	return &shipper.Release{
		ObjectMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID(uid),
			Labels: map[string]string{
				shipper.AppLabel:     appName,
				shipper.ReleaseLabel: name,
			},
			Annotations: map[string]string{
				shipper.ReleaseGenerationAnnotation: generation,
			},
		},
		Spec: shipper.ReleaseSpec{
			Environment: shipper.ReleaseEnvironment{
				Chart: shipper.Chart{
					Name:    "reviews-api",
					Version: "0.0.1",
					RepoURL: "localhost",
				},
			},
		},
	}
}

func buildInstallationTargetWithOwner(ownerName, ownerUID, namespace, appName string, clusters []string) *shipper.InstallationTarget {
	return &shipper.InstallationTarget{
		ObjectMeta: v1.ObjectMeta{
			Name:      ownerName,
			Namespace: namespace,
			OwnerReferences: []v1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Kind:       "Release",
					Name:       ownerName,
					UID:        types.UID(ownerUID),
				},
			},
			Labels: map[string]string{
				shipper.AppLabel:            appName,
				shipper.ReleaseLabel:        ownerName,
				shipper.HelmWorkaroundLabel: shipper.True,
			},
		},
		Spec: shipper.InstallationTargetSpec{
			Clusters: clusters,
		},
	}
}

func buildInstallationTarget(owner *shipper.Release, namespace, appName string, clusters []string) *shipper.InstallationTarget {
	return buildInstallationTargetWithOwner(owner.Name, string(owner.UID), namespace, appName, clusters)
}
