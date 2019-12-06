package traffic

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

// patchOperation represents a JSON PatchOperation in a very specific way.
// Using jsonpatch's types could be a possiblity, but there's no need to be
// generic in here.
type patchOperation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value string `json:"value"`
}

// shiftPodLabels ensures that the pods in podsToShift have the
// shipper.PodTrafficStatusLabel label set to the specified values.
func shiftPodLabels(
	clientset kubernetes.Interface,
	podsToShift map[string][]*corev1.Pod,
) error {
	for value, pods := range podsToShift {
		for _, pod := range pods {
			v, ok := pod.Labels[shipper.PodTrafficStatusLabel]
			if ok && v == value {
				continue
			}

			patch := patchPodTrafficStatusLabel(pod, value)
			_, err := clientset.CoreV1().Pods(pod.Namespace).
				Patch(pod.Name, types.JSONPatchType, patch)
			if err != nil {
				return shippererrors.
					NewKubeclientPatchError(pod.Namespace, pod.Name, err).
					WithCoreV1Kind("Pod")
			}
		}
	}

	return nil
}

// patchPodTrafficStatusLabel returns a JSON Patch that modifies the
// PodTrafficStatusLabel value of a given Pod.
func patchPodTrafficStatusLabel(pod *corev1.Pod, value string) []byte {
	var op string
	if _, ok := pod.Labels[shipper.PodTrafficStatusLabel]; ok {
		op = "replace"
	} else {
		op = "add"
	}

	patchList := []patchOperation{
		{
			Op:    op,
			Path:  fmt.Sprintf("/metadata/labels/%s", shipper.PodTrafficStatusLabel),
			Value: value,
		},
	}

	// Don't know what to do in here. From my perspective it is quite
	// unlikely that the json.Marshal operation above would fail since its
	// input should be a valid serializable value.
	patchBytes, _ := json.Marshal(patchList)

	return patchBytes
}
