package object

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Kind(object metav1.Object) string {
	objKind := strings.TrimPrefix(object.GetSelfLink(), "/apis/shipper.booking.com/v1alpha1/namespaces/")
	objKind = strings.Trim(objKind, object.GetNamespace())
	objKind = strings.Trim(objKind, object.GetName())
	objKind = strings.Trim(objKind, "/")
	return objKind
}
