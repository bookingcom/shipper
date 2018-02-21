package capacity

import (
	"encoding/json"
	"fmt"
	"math"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/labels"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
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
	return cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(old, new interface{}) {
			c.enqueueDeployment(new, clusterName)
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

	// Only react on this event if the deployment has a `release` label that matches with a release
	var release string
	var ok bool
	if release, ok = targetDeployment.GetLabels()[shipperv1.ReleaseLabel]; !ok {
		// This deployment is not one of ours, so don't do anything
		return nil
	}

	capacityTarget, err := c.getCapacityTargetForReleaseAndNamespace(release, namespace)
	if err != nil {
		return err
	}

	sadPods, err := c.getSadPodsForDeploymentOnCluster(targetDeployment, item.ClusterName)
	if err != nil {
		return err
	}

	return c.updateStatus(capacityTarget, item.ClusterName, targetDeployment.Status.AvailableReplicas, sadPods)
}

func (c *Controller) updateStatus(capacityTarget *shipperv1.CapacityTarget, clusterName string, availableReplicas int32, sadPods []shipperv1.PodStatus) error {
	var capacityTargetStatus shipperv1.CapacityTargetStatus

	// We loop over the statuses in capacityTarget.Status.
	// If the name matches the cluster name we want, we don't add it to the resulting array.
	// If not, we just add it as-is.
	// At the end, we just append our own object to the end of the result. Since we originally filtered out the object matching our target cluster, our object will replace the original object.
	// The reason we make our own results object instead of modifying the original one is that we will use the resulting `capacityTargetStatus` variable for a patch operation
	for _, clusterStatus := range capacityTarget.Status.Clusters {
		if clusterStatus.Name == clusterName {
			continue
		}

		capacityTargetStatus.Clusters = append(capacityTargetStatus.Clusters, clusterStatus)
	}

	release, err := c.getReleaseForCapacityTarget(capacityTarget)
	if err != nil {
		return err
	}

	achievedPercent := c.calculatePercentageFromAmount(release.Environment.Replicas, availableReplicas)

	clusterStatus := shipperv1.ClusterCapacityStatus{
		Name:              clusterName,
		AchievedPercent:   achievedPercent,
		AvailableReplicas: availableReplicas,
		SadPods:           sadPods,
	}

	capacityTargetStatus.Clusters = append(capacityTargetStatus.Clusters, clusterStatus)

	statusJson, err := json.Marshal(capacityTargetStatus)
	if err != nil {
		return err
	}

	json := fmt.Sprintf(`{"status": %s}`, string(statusJson))
	_, err = c.shipperclientset.ShipperV1().CapacityTargets(capacityTarget.Namespace).Patch(capacityTarget.Name, types.MergePatchType, []byte(json))
	return err
}

func (c Controller) getCapacityTargetForReleaseAndNamespace(release, namespace string) (*shipperv1.CapacityTarget, error) {
	selector := labels.NewSelector()

	requirement, err := labels.NewRequirement(shipperv1.ReleaseLabel, selection.Equals, []string{release})
	if err != nil {
		return nil, err
	}
	selector = selector.Add(*requirement)

	capacityTargets, err := c.shipperclientset.ShipperV1().CapacityTargets(namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}

	if len(capacityTargets.Items) != 1 {
		return nil, fmt.Errorf("Expected exactly 1 capacity target with the label %s, but found %d", release, len(capacityTargets.Items))
	}

	return &capacityTargets.Items[0], nil
}

func (c Controller) getSadPodsForDeploymentOnCluster(deployment *appsv1.Deployment, clusterName string) ([]shipperv1.PodStatus, error) {
	var sadPods []shipperv1.PodStatus

	client, err := c.clusterClientStore.GetClient(clusterName)
	if err != nil {
		return nil, err
	}

	selector := labels.NewSelector()

	var releaseValue string
	var ok bool
	if releaseValue, ok = deployment.GetLabels()[shipperv1.ReleaseLabel]; !ok {
		return nil, fmt.Errorf("Deployment %s/%s has no label called 'release'", deployment.Namespace, deployment.Name)
	}

	if releaseValue == "" {
		return nil, fmt.Errorf("Deployment %s/%s has an empty 'release' label", deployment.Namespace, deployment.Name)
	}

	requirement, err := labels.NewRequirement(shipperv1.ReleaseLabel, selection.Equals, []string{releaseValue})
	if err != nil {
		return nil, err
	}
	selector = selector.Add(*requirement)

	pods, err := client.CoreV1().Pods(deployment.Namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}

	for _, pod := range pods.Items {
		if condition, ok := c.getFalsePodCondition(pod); ok {
			sadPod := shipperv1.PodStatus{
				Name:       pod.Name,
				Condition:  *condition,
				Containers: pod.Status.ContainerStatuses,
			}

			sadPods = append(sadPods, sadPod)
		}
	}

	return sadPods, nil
}

func (c Controller) getFalsePodCondition(pod corev1.Pod) (*corev1.PodCondition, bool) {
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
