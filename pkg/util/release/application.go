package release

import (
	"fmt"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

func ApplicationNameForRelease(rel *shipperv1.Release) (string, error) {
	if len(rel.OwnerReferences) != 1 {
		return "", fmt.Errorf("release %q has a weird number of owners: %d", rel.Name, len(rel.OwnerReferences))
	}
	return rel.OwnerReferences[0].Name, nil
}
