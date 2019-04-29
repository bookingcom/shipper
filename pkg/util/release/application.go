package release

import (
	"fmt"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

// TODO(jgreff): use structured errors
func ApplicationNameForRelease(rel *shipper.Release) (string, error) {
	if len(rel.OwnerReferences) != 1 {
		return "", fmt.Errorf("release %q has a weird number of owners: %d", rel.Name, len(rel.OwnerReferences))
	}
	return rel.OwnerReferences[0].Name, nil
}
