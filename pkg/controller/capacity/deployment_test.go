package capacity

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func TestSummarizeSadPods(t *testing.T) {
	sadPods := []shipper.PodStatus{
		{
			InitContainers: []corev1.ContainerStatus{
				{
					Name:  "ready-init-container",
					Ready: true,
				},
				{
					Name:  "waiting-init-container",
					Ready: false,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "ImagePullBackOff",
						},
					},
				},
			},
			Containers: []corev1.ContainerStatus{
				{
					Name:  "ready-container",
					Ready: true,
				},
				{
					Name:  "waiting-container",
					Ready: false,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "ImagePullBackOff",
						},
					},
				},
				{
					Name:  "terminated-container",
					Ready: false,
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason: "Completed",
						},
					},
				},
			},
		},
		{
			InitContainers: []corev1.ContainerStatus{
				{
					Name:  "waiting-init-container", // but not really :D
					Ready: true,
				},
			},
			Containers: []corev1.ContainerStatus{
				{
					Name:  "waiting-container",
					Ready: false,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "ErrImagePull",
						},
					},
				},
			},
		},
	}

	expected := strings.Join([]string{
		`1x"terminated-container" containers with [Completed]`,
		`2x"waiting-container" containers with [ErrImagePull ImagePullBackOff]`,
		`1x"waiting-init-container" containers with [ImagePullBackOff]`,
	}, "; ")
	actual := summarizeSadPods(sadPods)
	if expected != actual {
		t.Fatalf(
			"summary does not match.\nexpected: %s\nactual:   %s",
			expected, actual)
	}
}
