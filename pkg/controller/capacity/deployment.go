package capacity

import (
	"fmt"
	"math"
	"sort"

	"github.com/golang/glog"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

type deploymentWorkqueueItem struct {
	Key         string
	ClusterName string
}

func (c *Controller) runDeploymentWorker() {
	for c.processNextDeploymentWorkItem() {
	}
}

func (c *Controller) processNextDeploymentWorkItem() bool {
	obj, shutdown := c.deploymentWorkqueue.Get()
	if shutdown {
		return false
	}

	defer c.deploymentWorkqueue.Done(obj)

	var (
		key deploymentWorkqueueItem
		ok  bool
	)

	if key, ok = obj.(deploymentWorkqueueItem); !ok {
		c.deploymentWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will retry: false): %#v", obj))
		return true
	}

	shouldRetry := false
	err := c.deploymentSyncHandler(key)

	if err != nil {
		shouldRetry = shippererrors.ShouldRetry(err)
		runtime.HandleError(fmt.Errorf("error syncing Deployment %q (will retry: %t): %s", key, shouldRetry, err))
	}

	if shouldRetry {
		if c.deploymentWorkqueue.NumRequeues(key) >= maxRetries {
			// Drop the Deployment's key out of the workqueue and thus reset its
			// backoff. This limits the time a "broken" object can hog a worker.
			glog.Warningf("Deployment %q has been retried too many times, dropping from the queue", key.Key)
			c.deploymentWorkqueue.Forget(key)

			return true
		}

		c.deploymentWorkqueue.AddRateLimited(key)

		return true
	}

	glog.V(4).Infof("Successfully synced Deployment %q", key.Key)
	c.deploymentWorkqueue.Forget(key)

	return true
}

func (c *Controller) enqueueDeployment(obj interface{}, clusterName string) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	item := deploymentWorkqueueItem{
		Key:         key,
		ClusterName: clusterName,
	}

	c.deploymentWorkqueue.Add(item)
}

func (c Controller) NewDeploymentResourceEventHandler(clusterName string) cache.ResourceEventHandler {
	return cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			deploy, ok := obj.(*appsv1.Deployment)
			if !ok {
				glog.Warningf("Received something that's not a appsv1/Deployment: %v", obj)
				return false
			}

			_, ok = deploy.GetLabels()[shipper.ReleaseLabel]

			return ok
		},
		Handler: cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(old, new interface{}) {
				c.enqueueDeployment(new, clusterName)
			},
		},
	}
}

func (c *Controller) deploymentSyncHandler(item deploymentWorkqueueItem) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(item.Key)
	if err != nil {
		return shippererrors.NewUnrecoverableError(err)
	}

	informerFactory, err := c.clusterClientStore.GetInformerFactory(item.ClusterName)
	if err != nil {
		return err
	}

	targetDeployment, err := informerFactory.Apps().V1().Deployments().Lister().Deployments(namespace).Get(name)
	if err != nil {
		return shippererrors.NewKubeclientGetError(namespace, name, err).
			WithShipperKind("Application")
	}

	// Using ReleaseLabel here instead of the full set of Deployment labels because
	// we can't guarantee that there isn't extra stuff there that was put directly
	// in the chart.
	// Also not using ObjectReference here because it would go over cluster
	// boundaries. While technically it's probably ok, I feel like it'd be abusing
	// the feature.
	release := targetDeployment.GetLabels()[shipper.ReleaseLabel]
	capacityTarget, err := c.getCapacityTargetForReleaseAndNamespace(release, namespace)
	if err != nil {
		return err
	}

	c.enqueueCapacityTarget(capacityTarget)

	return nil
}

func (c Controller) getCapacityTargetForReleaseAndNamespace(release, namespace string) (*shipper.CapacityTarget, error) {
	selector := labels.Set{shipper.ReleaseLabel: release}.AsSelector()
	capacityTargets, err := c.capacityTargetsLister.CapacityTargets(namespace).List(selector)
	if err != nil {
		return nil, shippererrors.NewKubeclientListError(
			shipper.SchemeGroupVersion.WithKind("CapacityTarget"),
			namespace, selector, err)
	}

	if l := len(capacityTargets); l != 1 {
		return nil, shippererrors.NewInvalidCapacityTargetError(release, l)
	}

	return capacityTargets[0], nil
}

func (c Controller) getSadPodsForDeploymentOnCluster(deployment *appsv1.Deployment, clusterName string) (numberOfPods, numberOfSadPods int, sadPodConditions []shipper.PodStatus, err error) {
	var sadPods []shipper.PodStatus

	informer, err := c.clusterClientStore.GetInformerFactory(clusterName)
	if err != nil {
		return 0, 0, nil, err
	}

	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return 0, 0, nil, shippererrors.NewUnrecoverableError(fmt.Errorf("failed to transform label selector %v into a selector: %s", deployment.Spec.Selector, err))
	}

	pods, err := informer.Core().V1().Pods().Lister().Pods(deployment.Namespace).List(selector)
	if err != nil {
		return 0, 0, nil, shippererrors.NewKubeclientListError(
			corev1.SchemeGroupVersion.WithKind("Pod"),
			deployment.Namespace, selector, err)
	}

	for _, pod := range pods {
		if len(sadPods) == SadPodLimit {
			break
		}

		if condition, ok := c.getFalsePodCondition(pod); ok {
			sadPod := shipper.PodStatus{
				Name:           pod.Name,
				Condition:      *condition,
				InitContainers: pod.Status.InitContainerStatuses,
				Containers:     pod.Status.ContainerStatuses,
			}

			sadPods = append(sadPods, sadPod)
		}
	}

	sort.Slice(sadPods, func(i, j int) bool {
		return sadPods[i].Name < sadPods[j].Name
	})

	return len(pods), len(sadPods), sadPods, nil
}

func (c Controller) getFalsePodCondition(pod *corev1.Pod) (*corev1.PodCondition, bool) {
	// The loop below finds a condition with the `status` set to "false", which
	// means there is something wrong with the pod.
	for _, condition := range pod.Status.Conditions {
		if condition.Status == corev1.ConditionFalse {
			return &condition, true
		}
	}

	return nil, false
}

func (c Controller) calculatePercentageFromAmount(total, amount int32) int32 {
	result := float64(amount) / float64(total) * 100

	return int32(math.Ceil(result))
}
