package object

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

func GetApplicationLabel(obj metav1.Object) (string, error) {
	application, ok := obj.GetLabels()[shipper.AppLabel]
	if !ok || len(application) == 0 {
		return "", shippererrors.NewMissingShipperLabelError(obj, shipper.AppLabel)
	}

	return application, nil
}

func GetReleaseLabel(obj metav1.Object) (string, error) {
	release, ok := obj.GetLabels()[shipper.ReleaseLabel]
	if !ok || len(release) == 0 {
		return "", shippererrors.NewMissingShipperLabelError(obj, shipper.ReleaseLabel)
	}

	return release, nil
}

func GetMigrationLabel(obj metav1.Object) (bool, error) {
	migrated, ok := obj.GetLabels()[shipper.MigrationLabel]
	if !ok || len(migrated) == 0 {
		return false, shippererrors.NewMissingShipperLabelError(obj, shipper.ReleaseLabel)
	}

	return migrated == "true", nil
}
