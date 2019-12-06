package traffic

import (
	"fmt"
	"testing"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestShiftPodLabels(t *testing.T) {
	lbl := shipper.PodTrafficStatusLabel
	podsToShift := map[string][]*corev1.Pod{
		shipper.Enabled: {
			pod("empty-to-enabled", map[string]string{}),
			pod("enabled-to-enabled", map[string]string{lbl: shipper.Enabled}),
			pod("disabled-to-enabled", map[string]string{lbl: shipper.Disabled}),
			pod("misc-to-enabled", map[string]string{"foo": "bar"}),
		},
		shipper.Disabled: {
			pod("empty-to-disabled", map[string]string{}),
			pod("enabled-to-disabled", map[string]string{lbl: shipper.Enabled}),
			pod("disabled-to-disabled", map[string]string{lbl: shipper.Disabled}),
			pod("misc-to-disabled", map[string]string{"foo": "bar"}),
		},
		"foobar": {
			pod("empty-to-foobar", map[string]string{}),
		},
	}

	clientset := kubefake.NewSimpleClientset()
	tracker := clientset.Tracker()
	for _, pods := range podsToShift {
		for _, pod := range pods {
			tracker.Add(pod)
		}
	}

	err := shiftPodLabels(clientset, podsToShift)
	if err != nil {
		t.Fatalf("unable to shift pod labels: %s", err)
	}

	for newLabelValue, pods := range podsToShift {
		for _, p := range pods {
			gvr := corev1.SchemeGroupVersion.WithResource("pods")
			obj, err := tracker.Get(gvr, shippertesting.TestNamespace, p.Name)
			if err != nil {
				panic(fmt.Sprintf("can't find pod %q: %s", p.Name, err))
			}

			pod := obj.(*corev1.Pod)

			expectedLabels := p.Labels
			p.Labels[shipper.PodTrafficStatusLabel] = newLabelValue

			actualLabels := pod.Labels
			eq, diff := shippertesting.DeepEqualDiff(expectedLabels, actualLabels)
			if !eq {
				t.Errorf("labels for pod %q differ from expected:\n%s", pod.Name, diff)
			}
		}
	}
}

func pod(name string, labels map[string]string) *corev1.Pod {
	// NOTE: apiserver's implementation of json patch differs from the one
	// in client-go's test reactor, so we have to cheat a little bit by
	// pretending that we're always doing a "replace" instead of an "add".
	// otherwise, it'll always complain with 'jsonpatch add operation does
	// not apply: doc is missing path:
	// "/metadata/labels/shipper-traffic-status"'
	if _, ok := labels[shipper.PodTrafficStatusLabel]; !ok {
		labels[shipper.PodTrafficStatusLabel] = ""
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: shippertesting.TestNamespace,
			Name:      name,
			Labels:    labels,
		},
	}
}
