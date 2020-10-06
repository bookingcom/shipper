package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

func MetaKey(object metav1.Object) string {
	key, _ := cache.MetaNamespaceKeyFunc(object)
	if key == "" {
		key = "<unknown>"
	}

	return key
}
