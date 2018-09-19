package label

import (
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

// FilterRelease makes a copy of the given set of labels without the 'release'
// labels. This is used when you want the group of labels which identify an
// application in general, rather than a specific release of that application.
func FilterRelease(source map[string]string) map[string]string {
	copy := map[string]string{}
	for k, v := range source {
		// Skip 'release' because we expect svcs to have a lifetime that spans
		// releases.
		if k == shipper.ReleaseLabel || k == shipper.ReleaseEnvironmentHashLabel {
			continue
		}
		copy[k] = v
	}
	return copy
}
