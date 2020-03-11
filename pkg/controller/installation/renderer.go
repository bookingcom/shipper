package installation

import (
	"fmt"
	"strings"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperchart "github.com/bookingcom/shipper/pkg/chart"
	shipperrepo "github.com/bookingcom/shipper/pkg/chart/repo"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
)

type kubeobj interface {
	runtime.Object
	GetLabels() map[string]string
	SetLabels(map[string]string)
}

func FetchAndRenderChart(
	chartFetcher shipperrepo.ChartFetcher,
	it *shipper.InstallationTarget,
) ([]runtime.Object, error) {
	chart, err := chartFetcher(it.Spec.Chart)
	if err != nil {
		return nil, err
	}

	manifests, err := shipperchart.Render(
		chart,
		it.GetName(),
		it.GetNamespace(),
		it.Spec.Values,
	)

	if err != nil {
		return nil, shippererrors.NewRenderManifestError(err)
	}

	return prepareObjects(it, manifests)
}

func prepareObjects(it *shipper.InstallationTarget, manifests []string) ([]runtime.Object, error) {
	shipperLabels := labels.Merge(labels.Set(it.Labels), labels.Set{
		shipper.InstallationTargetOwnerLabel: it.Name,
	})

	var (
		allServices          []*corev1.Service
		productionLBServices []*corev1.Service
	)

	preparedObjects := make([]runtime.Object, 0, len(manifests))
	for _, manifest := range manifests {
		decodedObj, _, err :=
			kubescheme.Codecs.
				UniversalDeserializer().
				Decode([]byte(manifest), nil, nil)

		if err != nil {
			return nil, shippererrors.NewDecodeManifestError("error decoding manifest: %s", err)
		}

		switch obj := decodedObj.(type) {
		case *appsv1.Deployment:
			// We need the Deployment in the chart to have a unique
			// name, meaning that different installations need to
			// generate Deployments with different names,
			// otherwise, we try to overwrite a previous
			// Deployment, and that fails with a "field is
			// immutable" error.
			deploymentName := obj.Name
			expectedName := it.Name
			if !strings.Contains(deploymentName, expectedName) {
				return nil, shippererrors.NewInvalidChartError(
					fmt.Sprintf("Deployment %q has invalid name."+
						" The name of the Deployment should be"+
						" templated with {{.Release.Name}}.",
						deploymentName),
				)
			}

			decodedObj = patchDeployment(obj, shipperLabels)
		case *corev1.Service:
			allServices = append(allServices, obj)

			lbValue, ok := obj.Labels[shipper.LBLabel]
			if ok && lbValue == shipper.LBForProduction {
				productionLBServices = append(productionLBServices, obj)
			}
		}

		obj := decodedObj.(kubeobj)
		obj.SetLabels(labels.Merge(
			obj.GetLabels(),
			shipperLabels,
		))

		preparedObjects = append(preparedObjects, obj)
	}

	// If we have observed only 1 Service object and it was not marked with
	// shipper-lb=production label, we can do it ourselves.
	if len(productionLBServices) == 0 && len(allServices) == 1 {
		productionLBServices = allServices
	}

	// If, after all, we still can not identify a single Service which will
	// be the production LB, there is nothing else to do rather than bail
	// out
	if len(productionLBServices) != 1 {
		return nil, shippererrors.NewInvalidChartError(
			fmt.Sprintf(
				"one and only one v1.Service object with label %q is required, but %d found instead",
				shipper.LBLabel, len(productionLBServices)))
	}

	err := patchService(it, productionLBServices[0])
	if err != nil {
		return nil, err
	}

	return preparedObjects, nil
}

func patchDeployment(d *appsv1.Deployment, labelsToInject map[string]string) runtime.Object {
	replicas := int32(0)
	d.Spec.Replicas = &replicas

	var newSelector *metav1.LabelSelector
	if d.Spec.Selector != nil {
		newSelector = d.Spec.Selector.DeepCopy()
	} else {
		newSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{},
		}
	}

	for k, v := range labelsToInject {
		newSelector.MatchLabels[k] = v
	}
	d.Spec.Selector = newSelector

	podTemplateLabels := d.Spec.Template.Labels
	for k, v := range labelsToInject {
		podTemplateLabels[k] = v
	}
	d.Spec.Template.SetLabels(podTemplateLabels)

	return d
}

func patchService(it *shipper.InstallationTarget, s *corev1.Service) error {
	if relName, ok := s.Spec.Selector[shipper.HelmReleaseLabel]; ok {
		v, ok := it.Labels[shipper.HelmWorkaroundLabel]
		if ok && v == shipper.True {
			// This selector label is native to helm-bootstrapped charts.
			// In order to make it work the shipper way, we remove the
			// label and proceed normally. With one little twist: the user
			// has to ask shipper to do it explicitly.
			if relName == it.Name {
				delete(s.Spec.Selector, shipper.HelmReleaseLabel)
			}
		} else if !ok || v == shipper.False {
			return shippererrors.NewInvalidChartError(
				fmt.Sprintf("The chart contains %q label in Service object %q. This will"+
					" break shipper traffic shifting logic. Consider adding the workaround"+
					" label %q: true to your Application object",
					shipper.HelmReleaseLabel, s.Name, shipper.HelmWorkaroundLabel))
		} else {
			return shippererrors.NewInvalidChartError(
				fmt.Sprintf("Unexpected value for label %q: %q. Expected values: %s/%s.",
					shipper.HelmWorkaroundLabel, v, shipper.True, shipper.False))
		}
	}

	s.Labels[shipper.LBLabel] = shipper.LBForProduction

	// Make sure service selector is safely defined
	if s.Spec.Selector == nil {
		s.Spec.Selector = make(map[string]string)
	}
	s.Spec.Selector[shipper.AppLabel] = s.Labels[shipper.AppLabel]
	s.Spec.Selector[shipper.PodTrafficStatusLabel] = shipper.Enabled

	return nil
}
