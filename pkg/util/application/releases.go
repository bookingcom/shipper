package application

import (
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"github.com/bookingcom/shipper/pkg/errors"
)

// GetContender returns the contender from the given Release slice. The slice
// is expected to be sorted by ascending generation.
func GetContender(appName string, rels []*shipperV1.Release) (*shipperV1.Release, error) {
	if len(rels) == 0 {
		return nil, errors.NewContenderNotFoundError(appName)
	}
	return rels[0], nil
}

// GetIncumbent returns the incumbent from the given Release slice. The slice
// is expected to be sorted by ascending generation.
//
// An incumbent release is the first release in this slice that is considered
// completed.
func GetIncumbent(appName string, rels []*shipperV1.Release) (*shipperV1.Release, error) {
	for _, r := range rels {
		if releaseutil.ReleaseComplete(r) {
			return r, nil
		}
	}
	return nil, errors.NewIncumbentNotFoundError(appName)
}

// ReleasesToApplicationHistory transforms the given Release slice into a
// string slice sorted by descending generation, suitable to be used set
// in ApplicationStatus.History.
func ReleasesToApplicationHistory(releases []*shipperV1.Release) []string {
	releases = releaseutil.SortByGenerationAscending(releases)
	names := make([]string, 0, len(releases))
	for _, rel := range releases {
		names = append(names, rel.GetName())
	}
	return names
}
