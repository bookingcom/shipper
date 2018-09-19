package janitor

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
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

func CreateConfigMapAnchor(it *shipper.InstallationTarget) (*corev1.ConfigMap, error) {
	anchorName, err := CreateAnchorName(it)
	if err != nil {
		return nil, err
	}
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
	return anchor, nil
}

func CreateAnchorName(it *shipper.InstallationTarget) (string, error) {
	return fmt.Sprintf("%s%s", it.Name, AnchorSuffix), nil
}
