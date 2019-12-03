package release

import (
	"fmt"
	"sort"
	"strconv"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/errors"
)

func GetGeneration(release *shipper.Release) (int, error) {
	rawGen, ok := release.GetAnnotations()[shipper.ReleaseGenerationAnnotation]
	if !ok {
		return 0, errors.NewMissingGenerationAnnotationError(release.Name)
	}

	generation, err := strconv.Atoi(rawGen)
	if err != nil {
		return 0, errors.NewInvalidGenerationAnnotationError(release.Name, err)
	}
	return generation, nil
}

func SetGeneration(release *shipper.Release, generation int) {
	release.Annotations[shipper.ReleaseGenerationAnnotation] = strconv.Itoa(generation)
}

func SetIteration(release *shipper.Release, iteration int) {
	release.Annotations[shipper.ReleaseTemplateIterationAnnotation] = strconv.Itoa(iteration)
}

func GetSiblingReleases(release *shipper.Release, appReleases []*shipper.Release) (*shipper.Release, *shipper.Release, error) {
	if len(appReleases) == 0 || release == nil {
		return nil, nil, fmt.Errorf("Empty release set provided")
	}

	var ancestor, predecessor *shipper.Release
	appReleases = SortByGenerationAscending(appReleases)

	relgen, err := GetGeneration(release)
	if err != nil {
		return nil, nil, err
	}

	var releaseIx = len(appReleases)
	for ix := range appReleases {
		if release.Name == appReleases[ix].Name &&
			release.Namespace == appReleases[ix].Namespace {
			releaseIx = ix
			break
		}
	}

	if releaseIx < len(appReleases) { // release is in the list
		if releaseIx+1 < len(appReleases) {
			ancestor = appReleases[releaseIx+1]
		}
		if releaseIx-1 >= 0 {
			predecessor = appReleases[releaseIx-1]
		}
		return predecessor, ancestor, nil
	}

	ancestorIx := sort.Search(len(appReleases), func(i int) bool {
		if release.Namespace == appReleases[i].Namespace &&
			release.ObjectMeta.OwnerReferences[0].Name == appReleases[i].ObjectMeta.OwnerReferences[0].Name {
			gen, _ := GetGeneration(appReleases[i])
			return relgen < gen
		}
		return false
	})

	if ancestorIx < len(appReleases) {
		ancestor = appReleases[ancestorIx]
		if ancestorIx-1 >= 0 {
			predecessor = appReleases[ancestorIx-1]
		}
	} else if gen, _ := GetGeneration(appReleases[len(appReleases)-1]); gen < relgen {
		ancestor = nil
		predecessor = appReleases[len(appReleases)-1]
	}

	return predecessor, ancestor, nil
}
