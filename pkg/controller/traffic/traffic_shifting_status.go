package traffic

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// TODO(jgreff): calculate achieved weight based on pods in the endpoint
// TODO(jgreff): maybe we don't need the pods themselves in Ready/NotReady

type TrafficShiftingStatus struct {
	LabeledPods   []*corev1.Pod
	UnlabeledPods []*corev1.Pod

	ReadyPods    int
	NotReadyPods int

	ReleasePods int

	AchievedPercentage float64

	podMap          map[string]*corev1.Pod
	podReadinessMap map[string]bool
}

func NewTrafficShiftingStatus(
	pods []*corev1.Pod,
	endpoints *corev1.Endpoints,
	trafficSelector labels.Selector,
	releaseSelector labels.Selector,
) *TrafficShiftingStatus {
	status := &TrafficShiftingStatus{
		podMap:          make(map[string]*corev1.Pod),
		podReadinessMap: make(map[string]bool),
	}

	for _, pod := range pods {
		podLabels := labels.Set(pod.Labels)
		if !releaseSelector.Matches(podLabels) {
			continue
		}

		status.podMap[pod.Name] = pod

		status.ReleasePods++

		if trafficSelector.Matches(podLabels) {
			status.LabeledPods = append(status.LabeledPods, pod)
		} else {
			status.UnlabeledPods = append(status.UnlabeledPods, pod)
		}
	}

	for _, subset := range endpoints.Subsets {
		status.markAddressReadiness(subset.Addresses, true)
		status.markAddressReadiness(subset.NotReadyAddresses, false)
	}

	for podName, podReady := range status.podReadinessMap {
		_, belongsToRelease := status.podMap[podName]

		if !belongsToRelease {
			continue
		}

		if podReady {
			status.ReadyPods++
		} else {
			status.NotReadyPods++
		}
	}

	status.AchievedPercentage = float64(status.ReadyPods) / float64(len(status.podReadinessMap))

	return status
}

// markAddressReadiness updates its internal map of pod readiness, by marking
// the pods from a list of EndpointAddress as either ready or not ready
// according to the markAs parameter.
func (s *TrafficShiftingStatus) markAddressReadiness(
	addresses []corev1.EndpointAddress,
	markAs bool,
) {
	for _, address := range addresses {
		target := address.TargetRef
		// Don't know what to do if the target is not a Pod, so
		// just skip it.
		if target.Kind != "Pod" {
			continue
		}

		s.podReadinessMap[target.Name] = markAs
	}
}
