package release

import (
	"fmt"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippercontroller "github.com/bookingcom/shipper/pkg/controller"
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
		runtime.HandleError(fmt.Errorf("invalid object key (will not retry): %#v", obj))
		return true
	}

	if shouldRetry := c.syncOneApplicationHandler(key); shouldRetry {
		if c.applicationWorkqueue.NumRequeues(key) >= maxRetries {
			glog.Warningf("Application %q has been retried too many times, droppping from the queue", key)
			c.applicationWorkqueue.Forget(key)

			return true
		}

		c.applicationWorkqueue.AddRateLimited(key)

		return true
	}

	c.applicationWorkqueue.Forget(obj)
	glog.V(4).Infof("Successfully synced Application %q", key)

	return true
}

// syncOneApplicationHandler processes application keys one-by-one. On this stage a
// release is expected to be scheduled. This handler instantiates a strategy
// executor and executes it.
func (c *Controller) syncOneApplicationHandler(key string) bool {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid object key (will not retry): %q", key))
		return noRetry
	}
	app, err := c.applicationLister.Applications(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.V(3).Infof("Application %q not found", key)
			return noRetry
		}

		runtime.HandleError(fmt.Errorf("failed to process Application %q (will retry): %s", key, err))

		return retry
	}

	glog.V(4).Infof("Fetching release pair for Application %q", key)
	incumbent, contender, err := c.getWorkingReleasePair(app)
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing Application %q (will retry): %s", key, err))
		return retry
	}

	glog.V(4).Infof("Building a strategy excecutor for Application %q", key)
	strategyExecutor, err := c.buildExecutor(incumbent, contender)
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing Application %q (will retry): %s", key, err))
		return retry
	}

	glog.V(4).Infof("Executing the strategy on Application %q", key)
	patches, transitions, err := strategyExecutor.Execute()
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing Application %q (will not retry): %s", key, err))

		return noRetry
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
		glog.V(4).Infof("Strategy verified, nothing to patch")
		return noRetry
	}

	glog.V(4).Infof("Strategy has been executed, applying patches")
	for _, patch := range patches {
		name, gvk, b := patch.PatchSpec()
		switch gvk.Kind {
		case "Release":
			if _, err := c.clientset.ShipperV1alpha1().Releases(namespace).Patch(name, types.MergePatchType, b); err != nil {
				runtime.HandleError(fmt.Errorf("error syncing Release for Application %q (will retry): %s", key, err))
				return retry
			}
		case "InstallationTarget":
			if _, err := c.clientset.ShipperV1alpha1().InstallationTargets(namespace).Patch(name, types.MergePatchType, b); err != nil {
				runtime.HandleError(fmt.Errorf("error syncing InstallationTarget for Application %q (will retry): %s", key, err))
				return retry
			}
		case "CapacityTarget":
			if _, err := c.clientset.ShipperV1alpha1().CapacityTargets(namespace).Patch(name, types.MergePatchType, b); err != nil {
				runtime.HandleError(fmt.Errorf("error syncing CapacityTarget for Application %q (will retry): %s", key, err))
				return retry
			}
		case "TrafficTarget":
			if _, err := c.clientset.ShipperV1alpha1().TrafficTargets(namespace).Patch(name, types.MergePatchType, b); err != nil {
				runtime.HandleError(fmt.Errorf("error syncing TrafficTarget for Application %q (will retry): %s", key, err))
				return retry
			}
		default:
			runtime.HandleError(fmt.Errorf("error syncing Application %q (will not retry): unknown GVK resource name: %s", key, gvk.Kind))
			return noRetry
		}
	}

	return noRetry
}

func (c *Controller) buildExecutor(incumbentRelease, contenderRelease *shipper.Release) (*Executor, error) {
	if !releaseutil.ReleaseScheduled(contenderRelease) {
		return nil, NewNotWorkingOnStrategyError(shippercontroller.MetaKey(contenderRelease))
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

func (c *Controller) sortedReleasesForApp(namespace, name string) ([]*shipper.Release, error) {
	selector := labels.Set{
		shipper.AppLabel: name,
	}.AsSelector()

	releases, err := c.releaseLister.Releases(namespace).List(selector)
	if err != nil {
		return nil, err
	}

	sorted, err := shippercontroller.SortReleasesByGeneration(releases)
	if err != nil {
		return nil, err
	}

	return sorted, nil
}

func (c *Controller) getWorkingReleasePair(app *shipper.Application) (*shipper.Release, *shipper.Release, error) {
	appReleases, err := c.sortedReleasesForApp(app.GetNamespace(), app.GetName())
	if err != nil {
		return nil, nil, err
	}

	if len(appReleases) == 0 {
		return nil, nil, fmt.Errorf(
			"zero release records in app %q: will not execute strategy",
			shippercontroller.MetaKey(app),
		)
	}

	// Walk backwards until we find a scheduled release. There may be pending
	// releases ahead of the actual contender, that's not we're looking for.
	var contender *shipper.Release
	for i := len(appReleases) - 1; i >= 0; i-- {
		if releaseutil.ReleaseScheduled(appReleases[i]) {
			contender = appReleases[i]
			break
		}
	}

	if contender == nil {
		return nil, nil, fmt.Errorf("couldn't find a contender for Application %q",
			shippercontroller.MetaKey(app))
	}

	var incumbent *shipper.Release
	// Walk backwards until we find an installed release that isn't the head of
	// history. For releases A -> B -> C, if B was never finished this allows C to
	// ignore it and let it get deleted so the transition is A->C.
	for i := len(appReleases) - 1; i >= 0; i-- {
		if releaseutil.ReleaseComplete(appReleases[i]) && contender != appReleases[i] {
			incumbent = appReleases[i]
			break
		}
	}

	// It is OK if incumbent is nil. It just means this is our first rollout.
	return incumbent, contender, nil
}
