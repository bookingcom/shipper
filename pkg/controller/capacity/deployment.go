package capacity

import (
	"fmt"
	"math"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

func (c Controller) NewDeploymentResourceEventHandler(clusterName string) cache.ResourceEventHandler {
	return cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			deploy, ok := obj.(*appsv1.Deployment)
			if !ok {
				klog.Warningf("Received something that's not a appsv1/Deployment: %v", obj)
				return false
			}

			_, ok = deploy.GetLabels()[shipper.ReleaseLabel]

			return ok
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				c.enqueueCapacityTargetFromDeployment(obj, clusterName)
			},
			UpdateFunc: func(old, new interface{}) {
				c.enqueueCapacityTargetFromDeployment(new, clusterName)
			},
			DeleteFunc: func(obj interface{}) {
				c.enqueueCapacityTargetFromDeployment(obj, clusterName)
			},
		},
	}
}

func (c *Controller) enqueueCapacityTargetFromDeployment(obj interface{}, clusterName string) {
	deployment, ok := obj.(*appsv1.Deployment)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a Deployment: %#v", obj))
		return
	}

	// Using ReleaseLabel here instead of the full set of Deployment labels because
	// we can't guarantee that there isn't extra stuff there that was put directly
	// in the chart.
	// Also not using ObjectReference here because it would go over cluster
	// boundaries. While technically it's probably ok, I feel like it'd be abusing
	// the feature.
	rel := deployment.GetLabels()[shipper.ReleaseLabel]
	ct, err := c.getCapacityTargetForReleaseAndNamespace(rel, deployment.GetNamespace())
	if err != nil {
		runtime.HandleError(fmt.Errorf("cannot get capacity target for release '%s/%s': %#v", rel, deployment.GetNamespace(), obj))
		return
	}

	c.enqueueCapacityTarget(ct)
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
