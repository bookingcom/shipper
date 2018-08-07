package controller

import (
	"sort"
	"strconv"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"github.com/bookingcom/shipper/pkg/errors"
)

func SortReleasesByGeneration(releases []*shipperv1.Release) ([]*shipperv1.Release, error) {
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

func GetReleaseGeneration(release *shipperv1.Release) (int, error) {
	rawGen, ok := release.GetAnnotations()[shipperv1.ReleaseGenerationAnnotation]
	if !ok {
		return 0, errors.NewMissingGenerationAnnotationError(release.Name)
	}

	generation, err := strconv.Atoi(rawGen)
	if err != nil {
		return 0, errors.NewInvalidGenerationAnnotationError(release.Name, err)
	}
	return generation, nil
}
