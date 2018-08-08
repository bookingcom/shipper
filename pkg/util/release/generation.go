package release

import (
	"strconv"

	"github.com/bookingcom/shipper/pkg/errors"
	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

func GetGeneration(release *shipperv1.Release) (int, error) {
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

func SetGeneration(release *shipperv1.Release, generation int) {
	release.Annotations[shipperv1.ReleaseGenerationAnnotation] = strconv.Itoa(generation)
}

func SetIteration(release *shipperv1.Release, iteration int) {
	release.Annotations[shipperv1.ReleaseTemplateIterationAnnotation] = strconv.Itoa(iteration)
}
