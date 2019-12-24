package installation

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	helmchart "k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperchart "github.com/bookingcom/shipper/pkg/chart"
	shipperrepo "github.com/bookingcom/shipper/pkg/chart/repo"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	"github.com/bookingcom/shipper/pkg/util/anchor"
)

type DynamicClientBuilderFunc func(gvk *schema.GroupVersionKind, restConfig *rest.Config, cluster *shipper.Cluster) dynamic.Interface

// Installer is an object that knows how to install Helm charts directly into
// Kubernetes clusters.
type Installer struct {
	installationTarget *shipper.InstallationTarget
	preparedObjects    []runtime.Object
}

// NewInstaller returns a new Installer.
func NewInstaller(
	chartFetcher shipperrepo.ChartFetcher,
	it *shipper.InstallationTarget,
) (*Installer, error) {
	chart, err := chartFetcher(it.Spec.Chart)
	if err != nil {
		return nil, err
	}

	manifests, err := renderManifests(it, chart)
	if err != nil {
		return nil, err
	}

	preparedObjects, err := prepareObjects(it, manifests)
	if err != nil {
		return nil, err
	}

	return &Installer{
		installationTarget: it,
		preparedObjects:    preparedObjects,
	}, nil
}

// renderManifests returns a list of rendered manifests for a given
// InstallationTarget and chart, or an error.
func renderManifests(it *shipper.InstallationTarget, chart *helmchart.Chart) ([]string, error) {
	rendered, err := shipperchart.Render(
		chart,
		it.GetName(),
		it.GetNamespace(),
		it.Spec.Values,
	)

	if err != nil {
		return nil, shippererrors.NewRenderManifestError(err)
	}

	for _, v := range rendered {
		klog.V(10).Infof("Rendered object:\n%s", v)
	}

	return rendered, nil
}

type kubeobj interface {
	runtime.Object
	GetLabels() map[string]string
	SetLabels(map[string]string)
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

// buildResourceClient returns a ResourceClient suitable to manipulate the kind
// of resource represented by the given GroupVersionKind at the given Cluster.
func (i *Installer) buildResourceClient(
	cluster *shipper.Cluster,
	client kubernetes.Interface,
	restConfig *rest.Config,
	dynamicClientBuilder DynamicClientBuilderFunc,
	gvk *schema.GroupVersionKind,
) (dynamic.ResourceInterface, error) {
	// From the list of resources the target cluster knows about, find the resource for the
	// kind of object we have at hand.
	gv := gvk.GroupVersion()
	resources, err := client.Discovery().ServerResourcesForGroupVersion(gv.String())
	if err != nil {
		return nil, shippererrors.NewKubeclientDiscoverError(gv, err)
	}

	var resource *metav1.APIResource
	for _, e := range resources.APIResources {
		if e.Kind == gvk.Kind {
			resource = &e
			break
		}
	}

	if resource == nil {
		err := fmt.Errorf("kind %s not found on the Kubernetes cluster", gvk.Kind)
		return nil, shippererrors.NewUnrecoverableError(err)
	}

	// If it gets to this point, it means we have a resource, so we can create a
	// client for it scoping to the application's namespace. The namespace can be
	// ignored if creating, for example, objects that aren't bound to a namespace.
	gvr := schema.GroupVersionResource{
		Group:    gvk.Group,
		Version:  gvk.Version,
		Resource: resource.Name,
	}

	dynamicClient := dynamicClientBuilder(gvk, restConfig, cluster)
	resourceClient := dynamicClient.Resource(gvr)
	if resource.Namespaced {
		return resourceClient.Namespace(i.installationTarget.Namespace), nil
	} else {
		return resourceClient, nil
	}
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
	s.Spec.Selector[shipper.AppLabel] = s.Labels[shipper.AppLabel]
	s.Spec.Selector[shipper.PodTrafficStatusLabel] = shipper.Enabled

	return nil
}

// install attempts to install the manifests on the specified cluster.
func (i *Installer) install(
	cluster *shipper.Cluster,
	client kubernetes.Interface,
	restConfig *rest.Config,
	dynamicClientBuilderFunc DynamicClientBuilderFunc,
) error {
	it := i.installationTarget

	var createdConfigMap *corev1.ConfigMap

	configMap := anchor.CreateConfigMapAnchor(it)
	// TODO(jgreff): use a lister insted of a bare client
	existingConfigMap, err := client.CoreV1().ConfigMaps(it.Namespace).Get(configMap.Name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return shippererrors.NewKubeclientGetError(it.Name, configMap.Name, err).
			WithCoreV1Kind("ConfigMap")
	} else if err != nil { // errors.IsNotFound(err) == true
		createdConfigMap, err = client.CoreV1().ConfigMaps(configMap.Namespace).Create(configMap)
		if err != nil {
			return shippererrors.NewKubeclientCreateError(configMap, err).
				WithCoreV1Kind("ConfigMap")
		}
	} else {
		createdConfigMap = existingConfigMap
	}

	ownerReference := anchor.ConfigMapAnchorToOwnerReference(createdConfigMap)

	for _, preparedObj := range i.preparedObjects {
		obj := &unstructured.Unstructured{}
		err = kubescheme.Scheme.Convert(preparedObj, obj, nil)
		if err != nil {
			return shippererrors.NewConvertUnstructuredError("error converting object to unstructured: %s", err)
		}

		name := obj.GetName()
		namespace := obj.GetNamespace()
		gvk := obj.GroupVersionKind()

		resourceClient, err := i.buildResourceClient(
			cluster,
			client,
			restConfig,
			dynamicClientBuilderFunc,
			&gvk,
		)
		if err != nil {
			return err
		}

		// "fetch-and-create-or-update" strategy in here; this is required to
		// overcome an issue in Kubernetes where a "create-or-update" strategy
		// leads to exceeding quotas when those are enabled very quickly,
		// since Kubernetes machinery first increase quota usage and then
		// attempts to create the resource, taking some time to re-sync
		// the quota information when objects can't be created since they
		// already exist.
		existingObj, err := resourceClient.Get(name, metav1.GetOptions{})

		// Any error other than NotFound is not recoverable from this point on.
		if err != nil && !errors.IsNotFound(err) {
			return shippererrors.
				NewKubeclientGetError(namespace, name, err).
				WithKind(gvk)
		}

		// If have an error here, it means it is NotFound, so proceed to
		// create the object on the application cluster.
		if err != nil {
			obj.SetOwnerReferences([]metav1.OwnerReference{ownerReference})
			_, err = resourceClient.Create(obj, metav1.CreateOptions{})
			if err != nil {
				return shippererrors.
					NewKubeclientCreateError(obj, err).
					WithKind(gvk)
			}
			continue
		}

		// We inject a Namespace object in the objects to be installed
		// for a particular InstallationTarget; we don't want to
		// continue if the Namespace already exists.
		if gvk.Kind == "Namespace" {
			continue
		}

		shouldUpdate, err := shouldUpdateObject(it, existingObj)
		if err != nil {
			return err
		} else if !shouldUpdate {
			continue
		}

		ownerReferenceFound := false
		for _, o := range existingObj.GetOwnerReferences() {
			if reflect.DeepEqual(o, ownerReference) {
				ownerReferenceFound = true
			}
		}
		if !ownerReferenceFound {
			ownerReferences := append(existingObj.GetOwnerReferences(), ownerReference)
			sort.Slice(ownerReferences, func(i, j int) bool {
				return ownerReferences[i].Name < ownerReferences[j].Name
			})
			existingObj.SetOwnerReferences(ownerReferences)
		}

		existingObj.SetLabels(obj.GetLabels())
		existingObj.SetAnnotations(obj.GetAnnotations())
		existingUnstructuredObj := existingObj.UnstructuredContent()
		newUnstructuredObj := obj.UnstructuredContent()

		if gvk.Kind == "Service" {
			// Copy over clusterIP from existing object's .spec to
			// the rendered one.
			if clusterIP, ok, err := unstructured.NestedString(existingUnstructuredObj, "spec", "clusterIP"); ok {
				if err != nil {
					return err
				}

				unstructured.SetNestedField(newUnstructuredObj, clusterIP, "spec", "clusterIP")
			}
		}

		unstructured.SetNestedField(existingUnstructuredObj, newUnstructuredObj["spec"], "spec")
		existingObj.SetUnstructuredContent(existingUnstructuredObj)

		if _, err := resourceClient.Update(existingObj, metav1.UpdateOptions{}); err != nil {
			return shippererrors.NewKubeclientUpdateError(obj, err).
				WithKind(gvk)
		}
	}

	return nil
}

// shouldUpdateObject detects whether the current iteration of the installer
// should update an object in the application cluster.
func shouldUpdateObject(it *shipper.InstallationTarget, obj *unstructured.Unstructured) (bool, error) {
	labels := obj.GetLabels()

	// If the InstallationTarget belongs to an app, we need to check that
	// the object belongs to the same app, otherwise we run the risk of
	// overwriting objects that this InstallationTarget is not responsible
	// for managing.
	app, itBelongsToApp := it.Labels[shipper.AppLabel]
	if itBelongsToApp {
		ownerApp, hasOwnerApp := labels[shipper.AppLabel]
		if !hasOwnerApp || ownerApp != app {
			err := shippererrors.NewInstallationTargetOwnershipError(obj)
			return false, err
		}
	}

	owner, ok := labels[shipper.InstallationTargetOwnerLabel]
	if !ok && !itBelongsToApp {
		// Object doesn't have an owner, so it might come from an older
		// version of shipper that used ReleaseLabel to signify
		// ownership instead. We bail out if it doesn't belong to an
		// app either, because that clearly means it wasn't created by
		// shipper at all.
		return false, shippererrors.NewInstallationTargetOwnershipError(obj)
	}

	// If the existing object is owned by the installation target, it
	// doesn't need any installation, and we don't want to update it
	// because it's common practice for users and other controllers to
	// patch objects, and expect that they'll remain changed.
	//
	// If it's owned by a different installation target, it'll be
	// overwritten only if it.Spec.CanOverride == true.
	if owner == it.Name || !it.Spec.CanOverride {
		return false, nil
	}

	return true, nil
}
