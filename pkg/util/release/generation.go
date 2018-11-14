package release

import (
	"strconv"

	"github.com/bookingcom/shipper/pkg/errors"
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
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
