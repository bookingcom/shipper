package filters

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func BelongsToRelease(obj interface{}) bool {
	kubeobj, ok := obj.(metav1.Object)
	if !ok {
		klog.Warningf("Received something that's not a metav1.Object: %v", obj)
		return false
	}

	_, ok = kubeobj.GetLabels()[shipper.ReleaseLabel]

	return ok
}

func BelongsToApp(obj interface{}) bool {
	kubeobj, ok := obj.(metav1.Object)
	if !ok {
		klog.Warningf("Received something that's not a metav1.Object: %v", obj)
		return false
	}

	_, ok = kubeobj.GetLabels()[shipper.AppLabel]

	return ok
}

func SliceContainsString(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
