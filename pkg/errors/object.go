package errors

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type MissingShipperLabelError struct {
	obj   metav1.Object
	label string
}

func (e MissingShipperLabelError) Error() string {
	return fmt.Sprintf(`Object "%s/%s" does not have required label %q`,
		e.obj.GetNamespace(), e.obj.GetName(), e.label)
}

func (e MissingShipperLabelError) ShouldRetry() bool {
	return false
}

func NewMissingShipperLabelError(obj metav1.Object, label string) MissingShipperLabelError {
	return MissingShipperLabelError{
		obj:   obj,
		label: label,
	}
}
