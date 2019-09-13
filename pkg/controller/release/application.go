package release

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/conditions"
	shippercontroller "github.com/bookingcom/shipper/pkg/controller"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	apputil "github.com/bookingcom/shipper/pkg/util/application"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

// processNextAppWorkItem pops a next item from the head of the application
// workqueue and passes it to the sync app handler. The returning bool is an
// indication if the process should go on normally.
func (c *Controller) processNextAppWorkItem() bool {
	obj, shutdown := c.applicationWorkqueue.Get()
	if shutdown {
		return false
	}

	defer c.applicationWorkqueue.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		c.applicationWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will retry: false): %#v", obj))
		return true
	}

	shouldRetry := false
	err := c.syncOneApplicationHandler(key)

	if err != nil {
		shouldRetry = shippererrors.ShouldRetry(err)
		runtime.HandleError(fmt.Errorf("error syncing Application %q (will retry: %t): %s", key, shouldRetry, err.Error()))
	}

	if shouldRetry {
		if c.applicationWorkqueue.NumRequeues(key) >= maxRetries {
			klog.Warningf("Application %q has been retried too many times, droppping from the queue", key)
			c.applicationWorkqueue.Forget(key)

			return true
		}

		c.applicationWorkqueue.AddRateLimited(key)

		return true
	}

	c.applicationWorkqueue.Forget(obj)
	klog.V(4).Infof("Successfully synced Application %q", key)

	return true
}

// syncOneApplicationHandler processes application keys one-by-one. On this stage a
// release is expected to be scheduled. This handler instantiates a strategy
// executor and executes it.
func (c *Controller) syncOneApplicationHandler(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return shippererrors.NewUnrecoverableError(err)
	}

	app, err := c.applicationLister.Applications(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.V(3).Infof("Application %q not found", key)
			return nil
		}

		return shippererrors.NewKubeclientGetError(namespace, name, err).
			WithShipperKind("Application")
	}

	klog.V(4).Infof("Fetching release pair for Application %q", key)
	incumbent, contender, err := c.getWorkingReleasePair(app)
	if err != nil {
		return err
	}

	klog.V(4).Infof("Building a strategy excecutor for Application %q", key)
	strategyExecutor, err := c.buildExecutor(incumbent, contender)
	if err != nil {
		return err
	}

	klog.V(4).Infof("Executing the strategy on Application %q", key)
	patches, transitions, err := strategyExecutor.Execute()
	if err != nil {
		releaseSyncedCond := apputil.NewApplicationCondition(
			shipper.ApplicationConditionTypeReleaseSynced,
			corev1.ConditionFalse,
			conditions.StrategyExecutionFailed,
			fmt.Sprintf("failed to execute application strategy: %q", err),
		)
		apputil.SetApplicationCondition(&app.Status, *releaseSyncedCond)
		_, err = c.clientset.ShipperV1alpha1().Applications(app.Namespace).Update(app)
		if err != nil {
			return shippererrors.NewKubeclientUpdateError(app, err).
				WithShipperKind("Application")
		}
		return err
	}

	for _, t := range transitions {
		c.recorder.Eventf(
			strategyExecutor.contender.release,
			corev1.EventTypeNormal,
			"ReleaseStateTransitioned",
			"Release %q had its state %q transitioned to %q",
			shippercontroller.MetaKey(strategyExecutor.contender.release),
			t.State,
			t.New,
		)
	}

	if len(patches) == 0 {
		klog.V(4).Infof("Strategy verified, nothing to patch")
		return nil
	}

	klog.V(4).Infof("Strategy has been executed, applying patches")
	for _, patch := range patches {
		name, gvk, b := patch.PatchSpec()

		var err error
		switch gvk.Kind {
		case "Release":
			_, err = c.clientset.ShipperV1alpha1().Releases(namespace).Patch(name, types.MergePatchType, b)
		case "InstallationTarget":
			_, err = c.clientset.ShipperV1alpha1().InstallationTargets(namespace).Patch(name, types.MergePatchType, b)
		case "CapacityTarget":
			_, err = c.clientset.ShipperV1alpha1().CapacityTargets(namespace).Patch(name, types.MergePatchType, b)
		case "TrafficTarget":
			_, err = c.clientset.ShipperV1alpha1().TrafficTargets(namespace).Patch(name, types.MergePatchType, b)
		default:
			return shippererrors.NewUnrecoverableError(fmt.Errorf("error syncing Application %q (will not retry): unknown GVK resource name: %s", key, gvk.Kind))
		}
		if err != nil {
			return shippererrors.NewKubeclientPatchError(namespace, name, err).WithKind(gvk)
		}
	}

	return nil
}

func (c *Controller) buildExecutor(incumbentRelease, contenderRelease *shipper.Release) (*Executor, error) {
	if !releaseutil.ReleaseScheduled(contenderRelease) {
		return nil, shippererrors.NewNotWorkingOnStrategyError(shippercontroller.MetaKey(contenderRelease))
	}

	contenderReleaseInfo, err := c.buildReleaseInfo(contenderRelease)
	if err != nil {
		return nil, err
	}

	strategy := *contenderReleaseInfo.release.Spec.Environment.Strategy

	// No incumbent, only this contender: a new application.
	if incumbentRelease == nil {
		return &Executor{
			contender: contenderReleaseInfo,
			recorder:  c.recorder,
			strategy:  strategy,
		}, nil
	}

	incumbentReleaseInfo, err := c.buildReleaseInfo(incumbentRelease)
	if err != nil {
		return nil, err
	}

	return &Executor{
		contender: contenderReleaseInfo,
		incumbent: incumbentReleaseInfo,
		recorder:  c.recorder,
		strategy:  strategy,
	}, nil
}

func (c *Controller) getWorkingReleasePair(app *shipper.Application) (*shipper.Release, *shipper.Release, error) {

	appReleases, err := c.releaseLister.Releases(app.Namespace).ReleasesForApplication(app.Name)
	if err != nil {
		return nil, nil, err
	}
	// Required by subsequent calls to GetContender and GetIncumbent.
	appReleases = releaseutil.SortByGenerationDescending(appReleases)

	contender, err := apputil.GetContender(app.Name, appReleases)
	if err != nil {
		return nil, nil, shippererrors.NewRecoverableError(err)
	}

	incumbent, err := apputil.GetIncumbent(app.Name, appReleases)
	if err != nil && !shippererrors.IsIncumbentNotFoundError(err) {
		return nil, nil, err
	}

	// It is OK if incumbent is nil. It just means this is our first rollout.
	return incumbent, contender, nil
}
