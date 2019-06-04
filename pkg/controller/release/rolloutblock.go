package release

import (
	"fmt"
	"strings"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	rolloutblockUtil "github.com/bookingcom/shipper/pkg/util/rolloutblock"
	stringUtil "github.com/bookingcom/shipper/pkg/util/string"
)

func (c *Controller) processNextRolloutBlockWorkItem() bool {
	// When a RolloutBlock is being deleted
	obj, shutdown := c.rbWorkqueue.Get()
	if shutdown {
		return false
	}

	defer c.rbWorkqueue.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		c.rbWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will retry: false): %#v", obj))
		return true
	}

	shouldRetry := false
	err := c.syncDeletedRolloutBlock(key)

	if err != nil {
		shouldRetry = shippererrors.ShouldRetry(err)
		runtime.HandleError(fmt.Errorf("error syncing Release %q (will retry: %t): %s", key, shouldRetry, err.Error()))
	}

	if shouldRetry {
		if c.rbWorkqueue.NumRequeues(key) >= maxRetries {
			// Drop the RolloutBlock's key out of the workqueue and thus reset its
			// backoff. This limits the time a "broken" object can hog a worker.
			glog.Warningf("RolloutBlock %q has been retried too many times, dropping from the queue", key)
			c.rbWorkqueue.Forget(key)

			return true
		}

		c.rbWorkqueue.AddRateLimited(key)

		return true
	}

	glog.V(4).Infof("Successfully synced RolloutBlock in Release %q", key)
	c.rbWorkqueue.Forget(obj)

	return true
}

func (c *Controller) syncDeletedRolloutBlock(key string) error {
	data := strings.Split(key, "*")
	if len(data) != 2 {
		return fmt.Errorf("could not separate RolloutBlock from overriding releases! ", key)
	}
	rbKey := data[0]
	releases := strings.Split(data[1], ",")

	for _, relFullName := range releases {
		err := c.removeRolloutBlockFromReleasesAnnotations(relFullName, rbKey)
		if err != nil {
			glog.V(3).Info(err.Error())
		}
	}

	return nil
}

func (c *Controller) removeRolloutBlockFromReleasesAnnotations(key string, rbFullName string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return shippererrors.NewUnrecoverableError(err)
	}

	release, err := c.releaseLister.Releases(ns).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			glog.V(3).Infof("Release %q has been deleted", key)
			return nil
		}

		return shippererrors.NewKubeclientGetError(ns, name, err).
			WithShipperKind("Release")
	}

	release = release.DeepCopy()

	if release.Annotations == nil {
		return nil
	}

	annotations, ok := release.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		return nil
	}

	annotationsArray := strings.Split(annotations, ",")
	annotationsArray = stringUtil.Delete(annotationsArray, rbFullName)
	if len(annotationsArray) == 0 {
		delete(release.Annotations, shipper.RolloutBlocksOverrideAnnotation)
	} else {
		release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = strings.Join(annotationsArray, ",")
	}

	_, err = c.clientset.ShipperV1alpha1().Releases(release.Namespace).Update(release)
	if err != nil {
		return shippererrors.NewKubeclientUpdateError(release, err).
			WithShipperKind("Release")
	}

	return nil
}

func (s *Scheduler) shouldBlockRollout(rel *shipper.Release) (bool, error, string) {
	nsRBs, err := s.rolloutBlockLister.RolloutBlocks(rel.Namespace).List(labels.Everything())
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing Application %q Because of namespace RolloutBlocks (will retry): %s", rel.Name, err))
	}

	gbRBs, err := s.rolloutBlockLister.RolloutBlocks(shipper.ShipperNamespace).List(labels.Everything())
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing Application %q Because of global RolloutBlocks (will retry): %s", rel.Name, err))
	}

	overrideRB, ok := rel.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		return false, nil, ""
	}

	overrideRolloutBlock, eventMessage, err := rolloutblockUtil.ShouldOverrideRolloutBlock(overrideRB, nsRBs, gbRBs)
	if err != nil {
		s.recorder.Event(rel, corev1.EventTypeWarning, "Overriding RolloutBlock", err.Error())
		runtime.HandleError(fmt.Errorf("error overriding rollout block %s", err.Error()))
		return true, err, ""
	}

	if !overrideRolloutBlock {
		s.recorder.Event(rel, corev1.EventTypeWarning, "RolloutBlock", eventMessage)
	} else {
		s.recorder.Event(rel, corev1.EventTypeNormal, "Override RolloutBlock", eventMessage)
	}

	return !overrideRolloutBlock, err, eventMessage
}

