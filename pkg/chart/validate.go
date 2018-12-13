package chart

import (
	"fmt"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	helmchart "k8s.io/helm/pkg/proto/hapi/chart"
)

func ValidateServices(decoded []runtime.Object) error {
	services := make([]*corev1.Service, 0)
	lbs := make([]*corev1.Service, 0)

	for _, decodedObj := range decoded {
		if svc, ok := decodedObj.(*corev1.Service); ok {
			services = append(services, svc)
			if lbValue, ok := svc.Labels[shipper.LBLabel]; ok && lbValue == shipper.LBForProduction {
				lbs = append(lbs, svc)
				if len(lbs) > 1 {
					return fmt.Errorf(
						fmt.Sprintf("Object %#v contains %q label, but %#v claims"+
							" it is the production LB. This looks like a misconfig:"+
							" only 1 service is allowed to be the production LB.",
							decodedObj, shipper.LBLabel, lbs[0]))
				}

			}
		}
	}

	// If there is only 1 service manifest and it was not labeled
	// as shipper-lb=production, we can do it automatically.
	if len(lbs) == 0 && len(services) == 1 {
		lbs = services
	}

	// If there is more or less than exactly 1 prod LB service,
	// shipper does not really know how to disambiguate this.
	// Returning an error.
	if len(lbs) != 1 {
		return fmt.Errorf(
			"one and only one v1.Service object with label %q is required, but %d found instead",
			shipper.LBLabel, len(lbs))
	}

	return nil
}

func Validate(helmChart *helmchart.Chart) error {
	manifests, renderErr := doRender(helmChart)
	if renderErr != nil {
		return renderErr
	}

	decoded, decodeErr := doDecode(manifests)
	if decodeErr != nil {
		return decodeErr
	}

	if err := ValidateServices(decoded); err != nil {
		return err
	}

	return nil
}

func doRender(helmChart *helmchart.Chart) ([]string, error) {
	manifests, renderErr := Render(
		helmChart,
		"release name",
		"release namespace",
		nil,
	)

	return manifests, renderErr
}

func doDecode(manifests []string) ([]runtime.Object, error) {
	decoded := make([]runtime.Object, 0, len(manifests))
	for _, manifest := range manifests {
		obj, _, err := kubescheme.Codecs.UniversalDeserializer().Decode([]byte(manifest), nil, nil)
		if err != nil {
			return nil, err
		}
		decoded = append(decoded, obj)
	}

	return decoded, nil
}
