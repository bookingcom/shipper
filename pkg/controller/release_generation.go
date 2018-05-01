package controller

import (
	"fmt"
	"sort"
	"strconv"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

func SortReleasesByGeneration(releases []*shipperv1.Release) ([]*shipperv1.Release, error) {
	if len(releases) == 0 {
		return releases, nil
	}

	// brutal schwartzian transform
	gens := map[string]int{}
	for _, rel := range releases {
		generation, err := GetReleaseGeneration(rel)
		if err != nil {
			return nil, err
		}
		gens[rel.GetName()] = generation
	}

	// this might be paranoid, but I'd rather not mutate the slice that the lister returns
	sortCopy := releases[:]

	sort.SliceStable(sortCopy, func(i, j int) bool {
		iGen := gens[sortCopy[i].GetName()]
		jGen := gens[sortCopy[j].GetName()]
		return iGen < jGen
	})

	return sortCopy, nil
}

func GetReleaseGeneration(release *shipperv1.Release) (int, error) {
	rawGen, ok := release.GetAnnotations()[shipperv1.ReleaseGenerationAnnotation]
	if !ok {
		return 0, fmt.Errorf("release %q missing generation annotation", release.GetName())
	}

	generation, err := strconv.Atoi(rawGen)
	if err != nil {
		return 0, fmt.Errorf("release %q invalid generation annotation: %s", release.GetName(), err)
	}
	return generation, nil
}
