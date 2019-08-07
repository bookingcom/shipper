package application

import (
	"k8s.io/helm/pkg/repo"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperrepo "github.com/bookingcom/shipper/pkg/chart/repo"
)

func ChartVersionResolved(app *shipper.Application) bool {
	chartspec := app.Spec.Template.Chart
	resVer, ok := app.Annotations[shipper.AppChartVersionResolvedAnnotation]
	if !ok || resVer != chartspec.Version {
		return false
	}

	chartName, ok := app.Annotations[shipper.AppChartNameAnnotation]
	if !ok || chartName != chartspec.Name {
		return false
	}

	return true
}

// This function modifies app object and populates it's annotations.
// The changes are not saved immediately and are delegated to the caller.
func ResolveChartVersion(app *shipper.Application, resolver shipperrepo.ChartVersionResolver) (*repo.ChartVersion, error) {
	cv, err := resolver(&app.Spec.Template.Chart)
	if err != nil {
		return nil, err
	}
	rawVer := app.Spec.Template.Chart.Version
	app.Spec.Template.Chart.Version = cv.Version

	UpdateChartNameAnnotation(app, cv.Name)
	UpdateChartVersionResolvedAnnotation(app, cv.Version)
	UpdateChartVersionRawAnnotation(app, rawVer)

	return cv, nil
}

func UpdateChartNameAnnotation(app *shipper.Application, name string) {
	app.Annotations[shipper.AppChartNameAnnotation] = name
}

func UpdateChartVersionResolvedAnnotation(app *shipper.Application, version string) {
	app.Annotations[shipper.AppChartVersionResolvedAnnotation] = version
}

func UpdateChartVersionRawAnnotation(app *shipper.Application, version string) {
	app.Annotations[shipper.AppChartVersionRawAnnotation] = version
}
