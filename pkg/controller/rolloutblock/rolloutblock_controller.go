package rolloutblock

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	stringUtil "github.com/bookingcom/shipper/pkg/util/string"
)

const (
	AgentName = "rolloutblock-controller"

	// maxRetries is the number of times an RolloutBlock will be retried before
	// we drop it out of the rolloutblock workqueue. The number is chosen with
	// the default rate limiter in mind. This results in the following backoff
	// times: 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s,
	// 5.1s, 10.2s.
	maxRetries = 11
)

// Controller is a Kubernetes controller that creates a rolloutblock object.
// It's main objective is to block application and releases rollout during
// an outage.
//
// RolloutBlock Controller has 2 primary workqueues: releases and applications.
type Controller struct {
	shipperClientset clientset.Interface
	recorder         record.EventRecorder

	applicationLister  shipperlisters.ApplicationLister
	applicationsSynced cache.InformerSynced

	releaseLister  shipperlisters.ReleaseLister
	releasesSynced cache.InformerSynced

	rolloutBlockLister shipperlisters.RolloutBlockLister
	rolloutBlockSynced cache.InformerSynced

	rolloutblockWorkqueue workqueue.RateLimitingInterface
}

// NewController returns a new RolloutBlock controller.
func NewController(
	shipperClientset clientset.Interface,
	informerFactory shipperinformers.SharedInformerFactory,
	recorder record.EventRecorder,
) *Controller {
	applicationInformer := informerFactory.Shipper().V1alpha1().Applications()
	releaseInformer := informerFactory.Shipper().V1alpha1().Releases()
	rolloutblockInformer := informerFactory.Shipper().V1alpha1().RolloutBlocks()

	glog.Info("Building a RolloutBlock controller")

	controller := &Controller{
		recorder:         recorder,
		shipperClientset: shipperClientset,

		applicationLister:  applicationInformer.Lister(),
		applicationsSynced: applicationInformer.Informer().HasSynced,

		releaseLister:  releaseInformer.Lister(),
		releasesSynced: releaseInformer.Informer().HasSynced,

		rolloutBlockLister: rolloutblockInformer.Lister(),
		rolloutBlockSynced: rolloutblockInformer.Informer().HasSynced,

		rolloutblockWorkqueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"rolloutblock_controller_rolloutblocks",
		),
	}

	glog.Info("Setting up event handlers")

	releaseInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.addRelease,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.updateRelease(oldObj, newObj)
			},
			DeleteFunc: controller.deleteRelease,
		})

	applicationInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.addApplication,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.updateApplication(oldObj, newObj)
			},
			DeleteFunc: controller.deleteApplication,
		})

	rolloutblockInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.addRolloutBlock,
		})

	return controller
}

func (c *Controller) addRelease(obj interface{}) {
	glog.Info("HILLA IN ADD RELEASE")
	rel, ok := obj.(*shipper.Release)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Release: %#v", obj))
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(rel)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	app, err := c.getAppFromRelease(rel)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	overrideRB, ok := app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		overrideRB = ""
		return
	}
	glog.Infof("HILLA these are the RBs that the release %s is overriding: %s", key, overrideRB)
	overrideRBs := strings.Split(overrideRB, ",")
	for _, rbKey := range overrideRBs {
		rolloutBlockUpdater := RolloutBlockUpdater{
			rbKey,
			OverridingRelease,
			key,
			false,
		}
		c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
	}
}

func (c *Controller) updateRelease(oldObj, newObj interface{}) {
	oldRel, ok := oldObj.(*shipper.Release)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Release: %#v", oldObj))
		return
	}

	newRel, ok := oldObj.(*shipper.Release)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Release: %#v", newObj))
		return
	}

	newOverride, newOk := newRel.Annotations[shipper.RolloutBlocksOverrideAnnotation]
	oldOverride, oldOk := oldRel.Annotations[shipper.RolloutBlocksOverrideAnnotation]
	if !oldOk {
		oldOverride = ""
	}
	if !newOk {
		newOverride = ""
	}

	relKey, err := cache.MetaNamespaceKeyFunc(newRel)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	newOverrides := strings.Split(newOverride, ",")
	oldOverrides := strings.Split(oldOverride, ",")
	glog.Infof("HILLA annotations before: %s", oldOverride)
	glog.Infof("HILLA annotations after : %s", newOverride)
	if stringUtil.Equal(newOverrides, oldOverrides) {
		for _, rbKey := range newOverrides {
			ns, name, err := cache.SplitMetaNamespaceKey(rbKey)
			if err != nil {
				runtime.HandleError(err)
				continue
			}
			rb, err := c.rolloutBlockLister.RolloutBlocks(ns).Get(name)
			if err != nil {
				runtime.HandleError(err)
				continue
			}
			if !stringUtil.Contains(rb.Status.Overrides.Release, relKey) {
				rolloutBlockUpdater := RolloutBlockUpdater{
					rbKey,
					OverridingRelease,
					relKey,
					false,
				}
				c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
			}
		}
	} else {
		for _, rbKey := range newOverrides {
			rolloutBlockUpdater := RolloutBlockUpdater{
				rbKey,
				OverridingRelease,
				relKey,
				false,
			}
			c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
		}
		removedRBs := stringUtil.Difference(oldOverrides, newOverrides)

		glog.Infof("HILLA this is the difference! %s", removedRBs)
		for _, rbKey := range removedRBs {
			rolloutBlockUpdater := RolloutBlockUpdater{
				rbKey,
				OverridingRelease,
				relKey,
				true,
			}
			c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
		}
	}
}

func (c *Controller) deleteRelease(obj interface{}) {
	release, ok := obj.(*shipper.Release)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Application: %#v", obj))
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(release)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	overrideRB, ok := release.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		overrideRB = ""
		return
	}

	overrideRBs := strings.Split(overrideRB, ",")
	for _, rbKey := range overrideRBs {
		rolloutBlockUpdater := RolloutBlockUpdater{
			rbKey,
			OverridingRelease,
			key,
			true,
		}
		c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
	}
}

func (c *Controller) addApplication(obj interface{}) {
	glog.Info("HILLA IN ADD APPLICATION")
	app, ok := obj.(*shipper.Application)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Application: %#v", obj))
		glog.Infof("HILLA this is the error: not a shipper application %#v", obj)
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(app)
	if err != nil {
		runtime.HandleError(err)
		glog.Infof("HILLA this is the error: %s", err.Error())
		return
	}

	overrideRB, ok := app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		overrideRB = ""
		glog.Infof("HILLA this app %s does not have overrides!", key)
		return
	}

	glog.Infof("HILLA trying to add app %q to queue, it overrides %s", key, overrideRB)
	overrideRBs := strings.Split(overrideRB, ",")
	for _, rbKey := range overrideRBs {
		rolloutBlockUpdater := RolloutBlockUpdater{
			rbKey,
			OverridingApplication,
			key,
			false,
		}

		glog.Infof("HILLA this is the rolloutblock updater: %s", rolloutBlockUpdater)
		c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
	}
}

func (c *Controller) deleteApplication(obj interface{}) {
	app, ok := obj.(*shipper.Application)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Application: %#v", obj))
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(app)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	overrideRB, ok := app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		overrideRB = ""
		return
	}

	overrideRBs := strings.Split(overrideRB, ",")
	for _, rbKey := range overrideRBs {
		rolloutBlockUpdater := RolloutBlockUpdater{
			rbKey,
			OverridingApplication,
			key,
			true,
		}
		c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
	}
}

func (c *Controller) updateApplication(oldObj, newObj interface{}) {
	oldApp, ok := oldObj.(*shipper.Application)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Application: %#v", oldObj))
		return
	}

	newApp, ok := oldObj.(*shipper.Application)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Application: %#v", newObj))
		return
	}

	newOverride, newOk := newApp.Annotations[shipper.RolloutBlocksOverrideAnnotation]
	oldOverride, oldOk := oldApp.Annotations[shipper.RolloutBlocksOverrideAnnotation]
	if !oldOk {
		oldOverride = ""
	}
	if !newOk {
		newOverride = ""
	}

	appKey, err := cache.MetaNamespaceKeyFunc(newApp)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	newOverrides := strings.Split(newOverride, ",")
	oldOverrides := strings.Split(oldOverride, ",")
	if stringUtil.Equal(newOverrides, oldOverrides) {
		for _, rbKey := range newOverrides {
			ns, name, err := cache.SplitMetaNamespaceKey(rbKey)
			if err != nil {
				runtime.HandleError(err)
				continue
			}
			rb, err := c.rolloutBlockLister.RolloutBlocks(ns).Get(name)
			if err != nil {
				runtime.HandleError(err)
				continue
			}
			if !stringUtil.Contains(rb.Status.Overrides.Application, appKey) {
				rolloutBlockUpdater := RolloutBlockUpdater{
					rbKey,
					OverridingApplication,
					appKey,
					false,
				}
				c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
			}
		}
	} else {
		for _, rbKey := range newOverrides {
			rolloutBlockUpdater := RolloutBlockUpdater{
				rbKey,
				OverridingApplication,
				appKey,
				false,
			}
			c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
		}
		removedRBs := stringUtil.Difference(oldOverrides, newOverrides)
		glog.Infof("HILLA this is the difference! %s", removedRBs)
		for _, rbKey := range removedRBs {
			rolloutBlockUpdater := RolloutBlockUpdater{
				rbKey,
				OverridingApplication,
				appKey,
				true,
			}
			c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
		}

	}
}

func (c *Controller) addRolloutBlock(obj interface{}) {
	rb, ok := obj.(*shipper.RolloutBlock)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.RolloutBlock: %#v", obj))
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(rb)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	rolloutBlockUpdater := RolloutBlockUpdater{
		key,
		NewRolloutBlockObject,
		"",
		true,
	}
	c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
}

// Run starts RolloutBlock controller workers and blocks until stopCh is
// closed.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.rolloutblockWorkqueue.ShutDown()

	glog.V(2).Info("Starting RolloutBlock controller")
	defer glog.V(2).Info("Shutting down RolloutBlock controller")

	if ok := cache.WaitForCacheSync(
		stopCh,
		c.applicationsSynced,
		c.releasesSynced,
	); !ok {
		runtime.HandleError(fmt.Errorf("failed to wait for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runRolloutBlockWorker, time.Second, stopCh)
	}

	glog.V(4).Info("Started RolloutBlock controller")

	<-stopCh
}

func (c *Controller) runRolloutBlockWorker() {
	for c.processNextRolloutBlockWorkItem() {
	}
}

func (c *Controller) processNextRolloutBlockWorkItem() bool {
	obj, shutdown := c.rolloutblockWorkqueue.Get()
	glog.Infof("HILLA processing next")
	if shutdown {
		return false
	}

	defer c.rolloutblockWorkqueue.Done(obj)

	var (
		key                 string
		ok                  bool
		rolloutBlockUpdater RolloutBlockUpdater
	)

	if rolloutBlockUpdater, ok = obj.(RolloutBlockUpdater); !ok {
		c.rolloutblockWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will retry: false): %#v", obj))
		return true
	}

	glog.Infof("HILLA pulled this rollout block updater: %s ", rolloutBlockUpdater)

	key = rolloutBlockUpdater.RolloutBlockKey
	updaterType := rolloutBlockUpdater.UpdaterType
	if updaterType == NewRolloutBlockObject {
		c.syncNewRolloutBlockObject(key)
		return true
	}

	overridingKey := rolloutBlockUpdater.OverridingObjectKey
	if rolloutBlockUpdater.IsDeletedObject {
		switch updaterType {
		case OverridingRelease:
			c.removeReleaseFromRolloutBlockStatus(overridingKey, key)
		case OverridingApplication:
			c.removeAppFromRolloutBlockStatus(overridingKey, key)
		}
	} else {
		switch updaterType {
		case OverridingRelease:
			c.addReleaseToRolloutBlockStatus(overridingKey, key)
		case OverridingApplication:
			c.addApplicationToRolloutBlockStatus(overridingKey, key)
		}
	}

	glog.Infof("HILLA finish processing this rollout block updater: %s ", rolloutBlockUpdater)
	return true
}

func (c *Controller) syncNewRolloutBlockObject(key string) {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	rb, err := c.rolloutBlockLister.RolloutBlocks(ns).Get(name)
	if err != nil {
		runtime.HandleError(err)
		return
	}
	rb.Status.Overrides.Application = []string{}
	rb.Status.Overrides.Release = []string{}
	_, err = c.shipperClientset.ShipperV1alpha1().RolloutBlocks(ns).Update(rb)
	if err != nil {
		runtime.HandleError(err)
	}

	if ns == shipper.ShipperNamespace {
		apps, err := c.applicationLister.List(labels.Everything())
		if err == nil {
			c.addMultipleApplicationsToRolloutBlocks(apps, key)
		} else {
			runtime.HandleError(err)
		}

		rels, err := c.releaseLister.List(labels.Everything())
		if err == nil {
			c.addMultipleReleasesToRolloutBlocks(rels, key)
		} else {
			runtime.HandleError(err)
		}
	} else {
		apps, err := c.applicationLister.Applications(ns).List(labels.Everything())
		if err == nil {
			c.addMultipleApplicationsToRolloutBlocks(apps, key)
		} else {
			runtime.HandleError(err)
		}

		rels, err := c.releaseLister.Releases(ns).List(labels.Everything())
		if err == nil {
			c.addMultipleReleasesToRolloutBlocks(rels, key)
		} else {
			runtime.HandleError(err)
		}
	}
}

func (c *Controller) addMultipleReleasesToRolloutBlocks(releases []*shipper.Release, rolloutBlockKey string) {
	for _, release := range releases {
		if release.DeletionTimestamp != nil {
			continue
		}

		relKey, err := cache.MetaNamespaceKeyFunc(release)
		if err != nil {
			runtime.HandleError(err)
			continue
		}

		overrideRB, ok := release.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
		if !ok {
			continue
		}

		overrideRBs := strings.Split(overrideRB, ",")
		for _, rbKey := range overrideRBs {
			if rbKey == rolloutBlockKey {
				c.addReleaseToRolloutBlockStatus(relKey, rbKey)
				return
			}
		}
	}
}

func (c *Controller) addMultipleApplicationsToRolloutBlocks(applications []*shipper.Application, rolloutBlockKey string) {
	for _, app := range applications {
		if app.DeletionTimestamp != nil {
			continue
		}

		appKey, err := cache.MetaNamespaceKeyFunc(app)
		if err != nil {
			runtime.HandleError(err)
			continue
		}

		overrideRB, ok := app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
		if !ok {
			continue
		}

		overrideRBs := strings.Split(overrideRB, ",")
		for _, rbKey := range overrideRBs {
			if rbKey == rolloutBlockKey {
				c.addApplicationToRolloutBlockStatus(appKey, rbKey)
				return
			}
		}
	}
}
