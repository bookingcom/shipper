package release

import (
	"fmt"

	"github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

func ApplicationNameForRelease(rel *v1.Release) (string, error) {
	if len(rel.OwnerReferences) != 1 {
		return "", fmt.Errorf("release %q has a weird number of owners: %d", rel.Name, len(rel.OwnerReferences))
	}
	return rel.OwnerReferences[0].Name, nil
}
