package janitor

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

func ConfigMapAnchorToOwnerReference(configMap *corev1.ConfigMap) metav1.OwnerReference {
	ownerReference := metav1.OwnerReference{
		UID:        configMap.UID,
		Name:       configMap.Name,
		Kind:       "ConfigMap",
		APIVersion: "v1",
	}
	return ownerReference
}

func BuildConfigMapAnchor(it *shipperv1.InstallationTarget) *corev1.ConfigMap {
	anchorName := buildAnchorName(it)
	anchor := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      anchorName,
			Namespace: it.Namespace,
			Labels:    it.Labels,
		},
		Data: map[string]string{
			InstallationTargetUID: string(it.UID),
		},
	}
	return anchor
}

func buildAnchorName(it *shipperv1.InstallationTarget) string {
	return fmt.Sprintf("%s%s", it.Name, AnchorSuffix)
}
