package release

import (
	"k8s.io/client-go/tools/cache"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

func ApplicationNameForRelease(rel *shipper.Release) (string, error) {
	if n := len(rel.OwnerReferences); n != 1 {
		key, _ := cache.MetaNamespaceKeyFunc(rel)
		return "", shippererrors.NewMultipleOwnerReferencesError(key, n)
	}

	return rel.OwnerReferences[0].Name, nil
}
