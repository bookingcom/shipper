package capacity

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/golang/glog"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"github.com/bookingcom/shipper/pkg/conditions"
	"github.com/bookingcom/shipper/pkg/controller"
)

type deploymentWorkqueueItem struct {
	Key         string
	ClusterName string
}

// runDeploymentWorker is a long-running function that will continually call the
// processNextDeploymentWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runDeploymentWorker() {
	for c.processNextDeploymentWorkItem() {
	}
}

// processNextWorkDeploymentItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextDeploymentWorkItem() bool {
	obj, shutdown := c.deploymentWorkqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.deploymentWorkqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.deploymentWorkqueue.Done(obj)

		var item deploymentWorkqueueItem
		var ok bool
		if item, ok = obj.(deploymentWorkqueueItem); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.deploymentWorkqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the deploymentSyncHandler, passing it the deploymentWorkQueueItem.
		if err := c.deploymentSyncHandler(item); err != nil {
			return fmt.Errorf("error syncing '%s': %s", item.Key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.deploymentWorkqueue.Forget(obj)
		glog.Infof("Successfully synced '%s'", item.Key)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

// enqueueDeployment takes a Deployment resource and converts it into a deploymentWorkqueueItem
// struct which is then put onto the work queue. This method should *not* be
// passed resources of any type other than Deployment.
func (c *Controller) enqueueDeployment(obj interface{}, clusterName string) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}

	item := deploymentWorkqueueItem{
		Key:         key,
		ClusterName: clusterName,
	}
	c.deploymentWorkqueue.AddRateLimited(item)
}

func (c Controller) NewDeploymentResourceEventHandler(clusterName string) cache.ResourceEventHandler {
	return cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			deploy, ok := obj.(*appsv1.Deployment)
			if !ok {
				glog.Warningf("Received something that's not a appsv1/Deployment: %v", obj)
				return false
			}

			_, ok = deploy.GetLabels()[shipperv1.ReleaseLabel]

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
		return err
	}

	// Get the informer for the cluster this item belongs to, and use the lister to fetch the deployment from the cache
	informerFactory, err := c.clusterClientStore.GetInformerFactory(item.ClusterName)
	if err != nil {
		return err
	}

	targetDeployment, err := informerFactory.Apps().V1().Deployments().Lister().Deployments(namespace).Get(name)
	if err != nil {
		return err
	}

	// Using ReleaseLabel here instead of the full set of Deployment labels because
	// we can't guarantee that there isn't extra stuff there that was put directly
	// in the chart.
	// Also not using ObjectReference here because it would go over cluster
	// boundaries. While technically it's probably ok, I feel like it'd be abusing
	// the feature.
	release := targetDeployment.GetLabels()[shipperv1.ReleaseLabel]
	capacityTarget, err := c.getCapacityTargetForReleaseAndNamespace(release, namespace)
	if err != nil {
		return err
	}

	// From this point on, we can report conditions since we have a CapacityTarget
	// object to work with.
	var clusterConditions []shipperv1.ClusterCapacityCondition
	for _, e := range capacityTarget.Status.Clusters {
		if e.Name == item.ClusterName {
			clusterConditions = e.Conditions
		}
	}

	podCount, sadPodsCount, sadPods, err := c.getSadPodsForDeploymentOnCluster(targetDeployment, item.ClusterName)
	if err != nil {
		clusterConditions = conditions.SetCapacityCondition(
			clusterConditions,
			shipperv1.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			conditions.ServerError,
			err.Error())
	}

	if sadPodsCount > 0 {
		clusterConditions = conditions.SetCapacityCondition(
			clusterConditions,
			shipperv1.ClusterConditionTypeReady,
			corev1.ConditionFalse,
			conditions.PodsNotReady,
			fmt.Sprintf("there are %d sad pods", sadPodsCount))
	} else if int(*targetDeployment.Spec.Replicas) != podCount {
		clusterConditions = conditions.SetCapacityCondition(
			clusterConditions,
			shipperv1.ClusterConditionTypeReady,
			corev1.ConditionFalse,
			conditions.WrongPodCount,
			fmt.Sprintf("expected %d replicas but have %d", *targetDeployment.Spec.Replicas, podCount))
	} else {
		clusterConditions = conditions.SetCapacityCondition(
			clusterConditions,
			shipperv1.ClusterConditionTypeReady,
			corev1.ConditionTrue,
			"", "")
	}

	return c.updateStatus(
		capacityTarget,
		item.ClusterName,
		targetDeployment.Status.AvailableReplicas,
		sadPods,
		clusterConditions)
}

func (c *Controller) updateStatus(
	capacityTarget *shipperv1.CapacityTarget,
	clusterName string,
	availableReplicas int32,
	sadPods []shipperv1.PodStatus,
	clusterConditions []shipperv1.ClusterCapacityCondition,
) error {
	var achievedPercent int32
	var capacityTargetStatus shipperv1.CapacityTargetStatus
	var replicas int

	// We loop over the statuses in capacityTarget.Status.  If the
	// name matches the cluster name we want, we don't add it to
	// the resulting array.  If not, we just add it as-is.  At the
	// end, we just append our own object to the end of the
	// result. Since we originally filtered out the object
	// matching our target cluster, our object will replace the
	// original object.  The reason we make our own results object
	// instead of modifying the original one is that we will use
	// the resulting `capacityTargetStatus` variable for a patch
	// operation
	for _, clusterStatus := range capacityTarget.Status.Clusters {
		if clusterStatus.Name == clusterName {
			continue
		}

		capacityTargetStatus.Clusters = append(capacityTargetStatus.Clusters, clusterStatus)
	}

	release, err := c.getReleaseForCapacityTarget(capacityTarget)
	if err != nil {
		switch err.(type) {
		case ReleaseIsGoneError:
			clusterConditions = conditions.SetCapacityCondition(
				clusterConditions,
				shipperv1.ClusterConditionTypeReady,
				corev1.ConditionFalse,
				conditions.MissingObjects,
				err.Error(),
			)
		case controller.MultipleOwnerReferencesError:
			clusterConditions = conditions.SetCapacityCondition(
				clusterConditions,
				shipperv1.ClusterConditionTypeReady,
				corev1.ConditionFalse,
				conditions.InvalidObjects,
				err.Error(),
			)
		default:
			// After investigating the code path of convertPercentageToReplicaCountForCluster(), the only
			// error it returns is a NotFound error when the release object associated with the capacity
			// target doesn't exist.
			clusterConditions = conditions.SetCapacityCondition(
				clusterConditions,
				shipperv1.ClusterConditionTypeReady,
				corev1.ConditionFalse,
				conditions.MissingObjects,
				err.Error(),
			)
		}

		goto End
	}

	replicas, err = strconv.Atoi(release.Annotations[shipperv1.ReleaseReplicasAnnotation])
	if err != nil {
		return err
	}

	achievedPercent = c.calculatePercentageFromAmount(int32(replicas), availableReplicas)

End:

	clusterStatus := shipperv1.ClusterCapacityStatus{
		Name:              clusterName,
		AchievedPercent:   achievedPercent,
		AvailableReplicas: availableReplicas,
		SadPods:           sadPods,
		Conditions:        clusterConditions,
	}

	capacityTargetStatus.Clusters = append(capacityTargetStatus.Clusters, clusterStatus)
	sort.Sort(byClusterName(capacityTargetStatus.Clusters))

	// doing this weird map assignment because we only want to
	// update the "status" field, and Kubernetes expects a
	// "status" key to be present in the patch
	patchData := map[string]shipperv1.CapacityTargetStatus{
		"status": capacityTargetStatus,
	}
	statusJson, err := json.Marshal(patchData)
	if err != nil {
		return err
	}

	_, err = c.shipperclientset.ShipperV1().CapacityTargets(capacityTarget.Namespace).Patch(capacityTarget.Name, types.MergePatchType, statusJson)
	if err == nil {
		c.recorder.Eventf(
			capacityTarget,
			corev1.EventTypeNormal,
			"CapacityStatusChanged",
			"Set %q status to %v",
			controller.MetaKey(capacityTarget),
			capacityTargetStatus,
		)
	} else {
		c.recorder.Eventf(
			capacityTarget,
			corev1.EventTypeWarning,
			"FailedCapacityStatusChange",
			err.Error(),
		)
	}

	return err
}

func (c Controller) getCapacityTargetForReleaseAndNamespace(release, namespace string) (*shipperv1.CapacityTarget, error) {
	selector := labels.Set{shipperv1.ReleaseLabel: release}.AsSelector()
	capacityTargets, err := c.capacityTargetsLister.CapacityTargets(namespace).List(selector)
	if err != nil {
		return nil, err
	}

	if l := len(capacityTargets); l != 1 {
		return nil, NewInvalidCapacityTargetError(release, l)
	}

	return capacityTargets[0], nil
}

func (c Controller) getSadPodsForDeploymentOnCluster(deployment *appsv1.Deployment, clusterName string) (numberOfPods, numberOfSadPods int, sadPodConditions []shipperv1.PodStatus, err error) {
	var sadPods []shipperv1.PodStatus

	informer, err := c.clusterClientStore.GetInformerFactory(clusterName)
	if err != nil {
		return 0, 0, nil, err
	}

	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("failed to transform label selector %v into a selector: %s", deployment.Spec.Selector, err)
	}

	pods, err := informer.Core().V1().Pods().Lister().Pods(deployment.Namespace).List(selector)
	if err != nil {
		return 0, 0, nil, err
	}

	for _, pod := range pods {
		// Stop adding sad pods to the status if we reach the
		// limit. This should, hopefully, be replaced with a
		// more heuristic way of reporting sad pods in the
		// future.
		if len(sadPods) == SadPodLimit {
			break
		}

		if condition, ok := c.getFalsePodCondition(pod); ok {
			sadPod := shipperv1.PodStatus{
				Name:           pod.Name,
				Condition:      *condition,
				InitContainers: pod.Status.InitContainerStatuses,
				Containers:     pod.Status.ContainerStatuses,
			}

			sadPods = append(sadPods, sadPod)
		}
	}

	return len(pods), len(sadPods), sadPods, nil
}

func (c Controller) getFalsePodCondition(pod *corev1.Pod) (*corev1.PodCondition, bool) {
	var sadCondition *corev1.PodCondition

	// The loop below finds a condition with the `status` set to
	// "false", which means there is something wrong with the pod.
	// The reason the loop is not returning as it finds the first
	// condition with the status of "false" is that we're testing the
	// assumption that there is only one condition with the status of
	// "false" at a time. That's why there is a log there for now.
	for _, condition := range pod.Status.Conditions {
		if condition.Status == corev1.ConditionFalse {
			if sadCondition == nil {
				sadCondition = &condition
			} else {
				glog.Errorf("Found 2 pod conditions with the status set to `false`. The first has a type of %s, and the second has a type of %s.", sadCondition.Type, condition.Type)
			}
		}
	}

	if sadCondition != nil {
		return sadCondition, true
	}

	return nil, false
}

func (c Controller) calculatePercentageFromAmount(total, amount int32) int32 {
	result := float64(amount) / float64(total) * 100

	return int32(math.Ceil(result))
}
