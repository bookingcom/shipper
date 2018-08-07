package janitor

import (
	"fmt"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

func ConfigMapAnchorToOwnerReference(configMap *coreV1.ConfigMap) metaV1.OwnerReference {
	ownerReference := metaV1.OwnerReference{
		UID:        configMap.UID,
		Name:       configMap.Name,
		Kind:       "ConfigMap",
		APIVersion: "v1",
	}
	return ownerReference
}

func BuildConfigMapAnchor(it *shipperV1.InstallationTarget) *coreV1.ConfigMap {
	anchorName := buildAnchorName(it)
	anchor := &coreV1.ConfigMap{
		TypeMeta: metaV1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metaV1.ObjectMeta{
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

func buildAnchorName(it *shipperV1.InstallationTarget) string {
	return fmt.Sprintf("%s%s", it.Name, AnchorSuffix)
}
