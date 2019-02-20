package capacity

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PodBuilder struct {
	podName              string
	podNamespace         string
	podLabels            map[string]string
	containerStatuses    []corev1.ContainerStatus
	podConditions        []corev1.PodCondition
	lastTerminationState corev1.ContainerState
}

func newPodBuilder(podName string, podNamespace string, podLabels map[string]string) *PodBuilder {
	return &PodBuilder{
		podName:      podName,
		podNamespace: podNamespace,
		podLabels:    podLabels,
	}
}

func (p *PodBuilder) SetName(name string) *PodBuilder {
	p.podName = name
	return p
}

func (p *PodBuilder) SetNamespace(namespace string) *PodBuilder {
	p.podNamespace = namespace
	return p
}

func (p *PodBuilder) SetLabels(labels map[string]string) *PodBuilder {
	p.podLabels = labels
	return p
}

func (p *PodBuilder) AddContainerStatus(containerName string, containerState corev1.ContainerState, restartCount int32, lastTerminatedState *corev1.ContainerState) *PodBuilder {
	containerStatus := corev1.ContainerStatus{Name: containerName, State: containerState, RestartCount: restartCount}
	if lastTerminatedState != nil {
		containerStatus.LastTerminationState = *lastTerminatedState
	}
	p.containerStatuses = append(p.containerStatuses, containerStatus)
	return p
}

func (p *PodBuilder) AddPodCondition(cond corev1.PodCondition) *PodBuilder {
	p.podConditions = append(p.podConditions, cond)
	return p
}

func (p *PodBuilder) Build() *corev1.Pod {

	sort.Slice(p.podConditions, func(i, j int) bool {
		return p.podConditions[i].Type < p.podConditions[j].Type
	})

	sort.Slice(p.containerStatuses, func(i, j int) bool {
		return p.containerStatuses[i].Name < p.containerStatuses[j].Name
	})

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.podName,
			Namespace: p.podNamespace,
			Labels:    p.podLabels,
		},
		Status: corev1.PodStatus{
			ContainerStatuses: p.containerStatuses,
			Conditions:        p.podConditions,
		},
	}
}
