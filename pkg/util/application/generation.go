package application

import (
	"strconv"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/errors"
)

func GetHighestObservedGeneration(app *shipper.Application) (int, error) {
	rawObserved, ok := app.Annotations[shipper.AppHighestObservedGenerationAnnotation]
	if !ok {
		return 0, nil
	}

	generation, err := strconv.Atoi(rawObserved)
	if err != nil {
		return 0, errors.NewApplicationAnnotationError(app.Name, shipper.AppHighestObservedGenerationAnnotation, err)
	}

	return generation, nil
}

func SetHighestObservedGeneration(app *shipper.Application, generation int) {
	app.Annotations[shipper.AppHighestObservedGenerationAnnotation] = strconv.Itoa(generation)
}

func CopyEnvironment(app *shipper.Application, rel *shipper.Release) {
	app.Spec.Template = *(rel.Spec.Environment.DeepCopy())
}
