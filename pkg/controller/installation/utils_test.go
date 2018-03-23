package installation

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"time"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"

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
)

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

// loadRelease loads a release from test data.
func loadRelease() *shipperV1.Release {
	release := &shipperV1.Release{}
	releaseYamlPath := filepath.Join("testdata", "release.yaml")

	if releaseRaw, err := ioutil.ReadFile(releaseYamlPath); err != nil {
		panic(err)
	} else if _, _, err = scheme.Codecs.UniversalDeserializer().Decode(releaseRaw, nil, release); err != nil {
		panic(err)
	}

	return release
}

// loadInstallationTarget loads an installation target from test data.
func loadInstallationTarget() *shipperV1.InstallationTarget {
	installationTarget := &shipperV1.InstallationTarget{}
	yamlPath := filepath.Join("testdata", "installationtarget.yaml")

	if bytes, err := ioutil.ReadFile(yamlPath); err != nil {
		panic(err)
	} else if _, _, err = scheme.Codecs.UniversalDeserializer().Decode(bytes, nil, installationTarget); err != nil {
		panic(err)
	}

	return installationTarget

}

// loadCluster returns a cluster.
func loadCluster(name string) *shipperV1.Cluster {
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
func initializeClients(apiResourceList []*v1.APIResourceList, objects ...runtime.Object) (
	kubernetes.Interface,
	*shipperfake.Clientset,
	*dynamicfake.FakeClient,
	DynamicClientBuilderFunc,
	shipperinformers.SharedInformerFactory,
) {
	fakeClient := kubefake.NewSimpleClientset()
	populateFakeDiscovery(fakeClient.Discovery(), apiResourceList)
	fakeDynamicClient := &dynamicfake.FakeClient{
		Fake: &kubetesting.Fake{},
	}
	fakeDynamicClientBuilder := func(kind *schema.GroupVersionKind, restConfig *rest.Config) dynamic.Interface {
		fakeDynamicClient.GroupVersion = kind.GroupVersion()
		return fakeDynamicClient
	}

	shipperclientset := shipperfake.NewSimpleClientset(objects...)
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
) *Controller {
	c := NewController(
		shipperclientset, shipperInformerFactory, fakeClientProvider, fakeDynamicClientBuilder)

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
