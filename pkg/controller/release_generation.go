package controller

import (
	"sort"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

func SortReleasesByGeneration(releases []*shipperv1.Release) ([]*shipperv1.Release, error) {
	if len(releases) == 0 {
		return releases, nil
	}

	// Brutal Schwartzian transform.
	gens := map[string]int{}
	for _, rel := range releases {
		generation, err := releaseutil.GetGeneration(rel)
		if err != nil {
			return nil, err
		}
		gens[rel.GetName()] = generation
	}

	// This might be paranoid, but I'd rather not mutate the slice that the lister
	// returns.
	sortCopy := releases[:]

	sort.SliceStable(sortCopy, func(i, j int) bool {
		iGen := gens[sortCopy[i].GetName()]
		jGen := gens[sortCopy[j].GetName()]
		return iGen < jGen
	})

	return sortCopy, nil
}
