package janitor

import (
	"fmt"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

func CreateConfigMapAnchor(it *shipperV1.InstallationTarget) (*coreV1.ConfigMap, error) {
	anchorName, err := CreateAnchorName(it)
	if err != nil {
		return nil, err
	}
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
	return anchor, nil
}

func CreateAnchorName(it *shipperV1.InstallationTarget) (string, error) {
	return fmt.Sprintf("%s-anchor", it.Name), nil
}