package application

import (
	"strconv"

	"github.com/bookingcom/shipper/pkg/errors"
	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

func GetHighestObservedGeneration(app *shipperv1.Application) (int, error) {
	rawObserved, ok := app.Annotations[shipperv1.AppHighestObservedGenerationAnnotation]
	if !ok {
		return 0, nil
	}

	generation, err := strconv.Atoi(rawObserved)
	if err != nil {
		return 0, errors.NewApplicationAnnotationError(app.Name, shipperv1.AppHighestObservedGenerationAnnotation, err)
	}

	return generation, nil
}

func SetHighestObservedGeneration(app *shipperv1.Application, generation int) {
	app.Annotations[shipperv1.AppHighestObservedGenerationAnnotation] = strconv.Itoa(generation)
}

func CopyEnvironment(app *shipperv1.Application, rel *shipperv1.Release) {
	app.Spec.Template = *(rel.Environment.DeepCopy())
}
