package janitor

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
)

// setup returns some objects that are used in several tests, basically to
// reduce boilerplate.
func setup(
	shipperObjects []runtime.Object,
	kubeObjects []runtime.Object,
) (
	*kubefake.Clientset,
	*shipperfake.Clientset,
	shipperinformers.SharedInformerFactory,
	kubeinformers.SharedInformerFactory,
) {
	kubeclientset := kubefake.NewSimpleClientset(kubeObjects...)
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeclientset, time.Second+0)
	shipperclientset := shipperfake.NewSimpleClientset(shipperObjects...)
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(shipperclientset, time.Second*0)
	return kubeclientset, shipperclientset, shipperInformerFactory, kubeInformerFactory
}

type FakeClusterClientStore struct {
	fakeClient            kubernetes.Interface
	sharedInformerFactory kubeinformers.SharedInformerFactory
	getClientShouldFail   bool
}

func (*FakeClusterClientStore) AddSubscriptionCallback(clusterclientstore.SubscriptionRegisterFunc) {
	// No-op.
}

func (*FakeClusterClientStore) AddEventHandlerCallback(clusterclientstore.EventHandlerRegisterFunc) {
	// No-op.
}

func (f *FakeClusterClientStore) GetClient(clusterName string) (kubernetes.Interface, error) {
	if f.getClientShouldFail {
		return nil, fmt.Errorf("Could not get client for cluster %q", clusterName)
	} else {
		return f.fakeClient, nil
	}
}

func (f *FakeClusterClientStore) GetInformerFactory(string) (kubeinformers.SharedInformerFactory, error) {
	return f.sharedInformerFactory, nil
}

// newController returns a janitor.Controller after it has started and
// waited for informer caches sync and there is something on the controller's
// workqueue.
func newController(
	populateWorkqueue bool,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	shipperclientset *shipperfake.Clientset,
	shipperInformerFactory shipperinformers.SharedInformerFactory,
	fakeClientStore clusterclientstore.Interface,
	fakeRecorder record.EventRecorder,
) *Controller {
	c := NewController(
		shipperclientset,
		shipperInformerFactory,
		fakeClientStore,
		fakeRecorder,
	)

	stopCh := make(chan struct{})
	defer close(stopCh)

	shipperInformerFactory.Start(stopCh)
	shipperInformerFactory.WaitForCacheSync(stopCh)

	kubeInformerFactory.Start(stopCh)
	kubeInformerFactory.WaitForCacheSync(stopCh)

	if populateWorkqueue {
		wait.PollUntil(
			10*time.Millisecond,
			func() (bool, error) { return c.workqueue.Len() >= 1, nil },
			stopCh,
		)
	}

	return c
}

// loadCluster returns a cluster.
func loadCluster(name string) *shipperv1.Cluster {
	return &shipperv1.Cluster{
		ObjectMeta: v1.ObjectMeta{
			Name: name,
		},
		Status: shipperv1.ClusterStatus{
			InService: true,
		},
	}
}

func loadInstallationTarget() *shipperv1.InstallationTarget {
	installationTarget := &shipperv1.InstallationTarget{}
	yamlPath := filepath.Join("testdata", "installationtarget.yaml")

	if bytes, err := ioutil.ReadFile(yamlPath); err != nil {
		panic(err)
	} else if _, _, err = scheme.Codecs.UniversalDeserializer().Decode(bytes, nil, installationTarget); err != nil {
		panic(err)
	}

	return installationTarget
}
