package controller

import (
	"fmt"
	"sort"
	"strconv"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func SortReleasesByGeneration(releases []*shipper.Release) ([]*shipper.Release, error) {
	if len(releases) == 0 {
		return releases, nil
	}

	// Brutal Schwartzian transform.
	gens := map[string]int{}
	for _, rel := range releases {
		generation, err := GetReleaseGeneration(rel)
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

func GetReleaseGeneration(release *shipper.Release) (int, error) {
	rawGen, ok := release.GetAnnotations()[shipper.ReleaseGenerationAnnotation]
	if !ok {
		return 0, fmt.Errorf("release %q missing generation annotation", release.GetName())
	}

	generation, err := strconv.Atoi(rawGen)
	if err != nil {
		return 0, fmt.Errorf("release %q invalid generation annotation: %s", release.GetName(), err)
	}
	return generation, nil
}
