package capacity

import (
	"fmt"
	"math"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

func (c *Controller) enqueueCapacityTargetFromDeployment(obj interface{}) {
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
		runtime.HandleError(fmt.Errorf("cannot get capacity target for release '%s/%s': %#v", rel, deployment.GetNamespace(), err))
		return
	}

	c.enqueueCapacityTarget(ct)
}

func (c Controller) getCapacityTargetForReleaseAndNamespace(release, namespace string) (*shipper.CapacityTarget, error) {
	selector := labels.Set{shipper.ReleaseLabel: release}.AsSelector()
	gvk := shipper.SchemeGroupVersion.WithKind("CapacityTarget")

	capacityTargets, err := c.capacityTargetsLister.CapacityTargets(namespace).List(selector)
	if err != nil {
		return nil, shippererrors.NewKubeclientListError(gvk, namespace, selector, err)
	}

	expected := 1
	if got := len(capacityTargets); got != 1 {
		return nil, shippererrors.NewUnexpectedObjectCountFromSelectorError(
			selector, gvk, expected, got)
	}

	return capacityTargets[0], nil
}

func (c Controller) getSadPods(pods []*corev1.Pod) []shipper.PodStatus {
	var sadPods []shipper.PodStatus
	for _, pod := range pods {
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

	return sadPods
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
