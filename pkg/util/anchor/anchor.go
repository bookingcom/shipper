package anchor

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

const (
	AnchorSuffix          = "-anchor"
	InstallationTargetUID = "InstallationTargetUID"
)

func BelongsToInstallationTarget(configMap *corev1.ConfigMap) bool {
	hasAnchorSuffix := strings.HasSuffix(configMap.GetName(), AnchorSuffix)
	_, hasUID := configMap.Data[InstallationTargetUID]

	return hasAnchorSuffix && hasUID
}

func ConfigMapAnchorToOwnerReference(configMap *corev1.ConfigMap) metav1.OwnerReference {
	ownerReference := metav1.OwnerReference{
		UID:        configMap.UID,
		Name:       configMap.Name,
		Kind:       "ConfigMap",
		APIVersion: "v1",
	}
	return ownerReference
}

func CreateConfigMapAnchor(it *shipper.InstallationTarget) *corev1.ConfigMap {
	anchor := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      CreateAnchorName(it),
			Namespace: it.Namespace,
			Labels:    it.Labels,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Kind:       "InstallationTarget",
					Name:       it.Name,
					UID:        it.UID,
				},
			},
		},
		Data: map[string]string{
			InstallationTargetUID: string(it.UID),
		},
	}
	return anchor
}

func CreateAnchorName(it *shipper.InstallationTarget) string {
	return fmt.Sprintf("%s%s", it.Name, AnchorSuffix)
}
