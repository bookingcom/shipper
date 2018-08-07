package errors

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

type kubeObject interface {
	GetObjectKind() schema.ObjectKind
	GetNamespace() string
	GetName() string
}

func printGVK(gvk schema.GroupVersionKind) string {
	return fmt.Sprintf("%s/%s/%s", gvk.Group, gvk.Version, gvk.Kind)
}
