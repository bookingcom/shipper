package capacity

import (
	"encoding/json"
	"fmt"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"

	"github.com/golang/glog"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	informerFactory := c.clusterInformerFactory[item.ClusterName]
	targetDeployment, err := informerFactory.Apps().V1().Deployments().Lister().Deployments(namespace).Get(name)
	if err != nil {
		return err
	}

	// Only react on this event if the deployment has a `release` label that matches with a release
	var release string
	var ok bool
	if release, ok = targetDeployment.GetLabels()["release"]; !ok {
		// This deployment is not one of ours, so don't do anything
		return nil
	}

	labelSelector := fmt.Sprintf("release=%s", release)
	capacityTargets, err := c.shipperclientset.ShipperV1().CapacityTargets(namespace).List(meta_v1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return err
	}

	if len(capacityTargets.Items) != 1 {
		return fmt.Errorf("Expected exactly 1 capacity target with the label %s, but found %d", release, len(capacityTargets.Items))
	}

	capacityTarget := capacityTargets.Items[0]
	glog.Infof("Got %d available replicas!", targetDeployment.Status.AvailableReplicas)
	err = c.updateStatus(capacityTarget, item.ClusterName, uint(targetDeployment.Status.AvailableReplicas))
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) updateStatus(capacityTarget shipperv1.CapacityTarget, name string, achievedReplicas uint) error {
	var capacityTargetStatus shipperv1.CapacityTargetStatus
	foundClusterStatus := false

	// We loop over the statuses in capacityTarget.Status.
	// If the name matches the cluster name we want, we update the status before adding it to `capacityTargetStatus`.
	// If not, we just add it as-is.
	// The reason we do it this way is that we will use the resulting `capacityTargetStatus` variable for a patch operation
	for _, clusterStatus := range capacityTarget.Status.Clusters {
		if clusterStatus.Name == name {
			foundClusterStatus = true
			glog.Infof("Found cluster %s! Setting available replicas to %d.", clusterStatus.Name, achievedReplicas)
			clusterStatus.AchievedReplicas = achievedReplicas
		}

		capacityTargetStatus.Clusters = append(capacityTargetStatus.Clusters, clusterStatus)
	}

	if foundClusterStatus != true {
		// there hasn't been an update about this cluster before, so manually add it
		clusterStatus := shipperv1.ClusterCapacityStatus{
			Name:             name,
			AchievedReplicas: achievedReplicas,
			Status:           "true",
		}

		capacityTargetStatus.Clusters = append(capacityTargetStatus.Clusters, clusterStatus)
	}

	statusJson, err := json.Marshal(capacityTargetStatus)
	if err != nil {
		return err
	}

	json := fmt.Sprintf(`{"status": %s}`, string(statusJson))
	glog.Info(json)
	_, err = c.shipperclientset.ShipperV1().CapacityTargets(capacityTarget.Namespace).Patch(capacityTarget.Name, types.MergePatchType, []byte(json))
	if err != nil {
		return err
	}

	return nil
}
