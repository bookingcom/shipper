package janitor

import (
	"time"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	shipperclient "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	shipperworkqueue "github.com/bookingcom/shipper/pkg/workqueue"
)

const (
	AgentName             = "janitor-controller"
	InstallationTargetUID = "InstallationTargetUID"
)

type Controller struct {
	shipperClientset   shipperclient.Interface
	workqueue          workqueue.RateLimitingInterface
	clusterClientStore clusterclientstore.Interface
	recorder           record.EventRecorder
}

func NewController(
	shipperclientset shipperclient.Interface,
	shipperInformerFactory shipperinformers.SharedInformerFactory,
	store clusterclientstore.Interface,
	recorder record.EventRecorder,
) *Controller {
	controller := &Controller{
		recorder:           recorder,
		workqueue:          workqueue.NewNamedRateLimitingQueue(shipperworkqueue.NewDefaultControllerRateLimiter(), "janitor_controller_installationtargets"),
		clusterClientStore: store,
		shipperClientset:   shipperclientset,
	}

	return controller
}

func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.V(2).Info("Starting Janitor controller")
	defer klog.V(2).Info("Shutting down Janitor controller")

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	klog.V(4).Info("Started Janitor controller")

	<-stopCh
}

func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()
	defer c.workqueue.Done(obj)

	if shutdown {
		return false
	}

	// TODO(jgreff): the janitor controller no longer makes sense. I'll
	// come back and fill this up once the new installation controller is
	// up and running.

	return true
}
