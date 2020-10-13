package filters

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/util/anchor"
)

func BelongsToRelease(obj interface{}) bool {
	kubeobj, ok := obj.(metav1.Object)
	if !ok {
		klog.Warningf("Received something that's not a metav1/Object: %v", obj)
		return false
	}

	_, ok = kubeobj.GetLabels()[shipper.ReleaseLabel]

	return ok
}

func BelongsToApp(obj interface{}) bool {
	kubeobj, ok := obj.(metav1.Object)
	if !ok {
		klog.Warningf("Received something that's not a metav1/Object: %v", obj)
		return false
	}

	_, ok = kubeobj.GetLabels()[shipper.AppLabel]

	return ok
}

func BelongsToInstallationTarget(obj interface{}) bool {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		klog.Warningf("Received something that's not a corev1/ConfigMap: %v", obj)
		return false
	}

	return BelongsToRelease(cm) && anchor.BelongsToInstallationTarget(cm)
}

func SliceContainsString(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
