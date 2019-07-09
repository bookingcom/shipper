package installation

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/golang/glog"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperchart "github.com/bookingcom/shipper/pkg/chart"
	shipperrepo "github.com/bookingcom/shipper/pkg/chart/repo"
	"github.com/bookingcom/shipper/pkg/controller/janitor"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

type DynamicClientBuilderFunc func(gvk *schema.GroupVersionKind, restConfig *rest.Config, cluster *shipper.Cluster) dynamic.Interface

// Installer is an object that knows how to install Helm charts directly into
// Kubernetes clusters.
type Installer struct {
	chartFetcher shipperrepo.ChartFetcher

	InstallationTarget *shipper.InstallationTarget
	Scheme             *runtime.Scheme
}

// NewInstaller returns a new Installer.
func NewInstaller(
	chartFetcher shipperrepo.ChartFetcher,
	it *shipper.InstallationTarget,
) *Installer {
	return &Installer{
		chartFetcher:       chartFetcher,
		InstallationTarget: it,
		Scheme:             kubescheme.Scheme,
	}
}

// renderManifests returns a list of rendered manifests for the given
// InstallationTarget and cluster, or an error.
func (i *Installer) renderManifests(_ *shipper.Cluster) ([]string, error) {
	it := i.InstallationTarget
	chart, err := i.chartFetcher(it.Spec.Chart)
	if err != nil {
		return nil, err
	}

	rendered, err := shipperchart.Render(
		chart,
		it.GetName(),
		it.GetNamespace(),
		it.Spec.Values,
	)

	if err != nil {
		err = shippererrors.NewRenderManifestError(err)
	}

	for _, v := range rendered {
		glog.V(10).Infof("Rendered object:\n%s", v)
	}

	return rendered, err
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
	dynamicClient := dynamicClientBuilder(gvk, restConfig, cluster)

	// From the list of resources the target cluster knows about, find the resource for the
	// kind of object we have at hand.
	var resource *metav1.APIResource
	gv := gvk.GroupVersion()
	resources, err := client.Discovery().ServerResourcesForGroupVersion(gv.String())
	if err != nil {
		return nil, shippererrors.NewKubeclientDiscoverError(gvk.GroupVersion(), err)
	}

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

	resourceClient := dynamicClient.Resource(gvr)
	if resource.Namespaced {
		return resourceClient.Namespace(i.InstallationTarget.Namespace), nil
	} else {
		return resourceClient, nil
	}
}

func (i *Installer) patchDeployment(
	d *appsv1.Deployment,
	labelsToInject map[string]string,
	ownerReference *metav1.OwnerReference,
) (runtime.Object, error) {

	d.OwnerReferences = []metav1.OwnerReference{*ownerReference}

	replicas := int32(0)
	d.Spec.Replicas = &replicas

	newLabels := d.Labels
	if newLabels == nil {
		newLabels = map[string]string{}
	}

	for k, v := range labelsToInject {
		newLabels[k] = v
	}
	d.SetLabels(newLabels)

	// Patch .spec.selector
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

	return d, nil
}

func (i *Installer) patchService(
	s *corev1.Service,
	labelsToInject map[string]string,
	ownerReference *metav1.OwnerReference,
) (runtime.Object, error) {

	// TODO(btyler): this check and this error are a blocker for using Releases
	// without Applications. I expect we should just blanket-apply all of the
	// Release's labels and cope with the fact that some of those are going to be
	// a little weird on application-lifetime objects like stably named Services
	requiredLabels := []string{shipper.AppLabel}
	for _, label := range requiredLabels {
		if _, ok := labelsToInject[label]; !ok {
			return nil, shippererrors.NewInvalidChartError(
				fmt.Sprintf(
					"Service is missing label %q. This means the Release is also missing it. You might be trying to use Releases without Applications, Shipper isn't ready for this yet.",
					label,
				),
			)
		}
	}

	// Those are modified regardless.
	s.OwnerReferences = []metav1.OwnerReference{*ownerReference}
	s.Labels = mergeLabels(s.Labels, labelsToInject)

	return s, nil
}

func (i *Installer) modifyServiceSelector(
	s *corev1.Service,
) (runtime.Object, error) {

	labels := s.Labels
	appName, ok := labels[shipper.AppLabel]
	if !ok {
		return nil, shippererrors.NewInvalidChartError(
			fmt.Sprintf("A service object metadata is expected to contain %q label, none found",
				shipper.AppLabel))
	}

	// We are interested only in patching Services properly identified by our specific label
	if lbValue, ok := labels[shipper.LBLabel]; ok && lbValue == shipper.LBForProduction {
		s.Spec.Selector[shipper.PodTrafficStatusLabel] = shipper.Enabled
	}

	if relName, ok := s.Spec.Selector[shipper.HelmReleaseLabel]; ok {
		if v, ok := i.InstallationTarget.Labels[shipper.HelmWorkaroundLabel]; ok && v == shipper.True {
			// This selector label is native to helm-bootstrapped charts.
			// In order to make it work the shipper way, we remove the
			// label and proceed normally. With one little twist: the user
			// has to ask shipper to do it explicitly.
			if relName == i.InstallationTarget.Name {
				delete(s.Spec.Selector, shipper.HelmReleaseLabel)
			}
		} else if !ok || v == shipper.False {
			return nil, shippererrors.NewInvalidChartError(
				fmt.Sprintf("The chart contains %q label in Service object %q. This will"+
					" break shipper traffic shifting logic. Consider adding the workaround"+
					" label %q: true to your Application object",
					shipper.HelmReleaseLabel, s.Name, shipper.HelmWorkaroundLabel))

		} else {
			return nil, shippererrors.NewInvalidChartError(
				fmt.Sprintf("Unexpected value for label %q: %q. Expected values: %s/%s.",
					shipper.HelmWorkaroundLabel, v, shipper.True, shipper.False))
		}
	}

	s.Spec.Selector[shipper.AppLabel] = appName

	return s, nil
}

func (i *Installer) patchUnstructured(
	o *unstructured.Unstructured,
	labelsToInject map[string]string,
	ownerReference *metav1.OwnerReference,
) (runtime.Object, error) {

	o.SetOwnerReferences([]metav1.OwnerReference{*ownerReference})

	labels := o.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	for k, v := range labelsToInject {
		labels[k] = v
	}
	o.SetLabels(labels)
	return o, nil
}

func (i *Installer) patchObject(
	object runtime.Object,
	labelsToInject map[string]string,
	ownerReference *metav1.OwnerReference,
) (runtime.Object, error) {
	switch o := object.(type) {
	case *appsv1.Deployment:
		return i.patchDeployment(o, labelsToInject, ownerReference)
	case *corev1.Service:
		return i.patchService(o, labelsToInject, ownerReference)
	default:
		unstructuredObj := &unstructured.Unstructured{}
		err := i.Scheme.Convert(object, unstructuredObj, nil)
		if err != nil {
			return nil, shippererrors.NewConvertUnstructuredError("error converting object to unstructured: %s", err)
		}
		return i.patchUnstructured(unstructuredObj, labelsToInject, ownerReference)
	}
}

// installManifests attempts to install the manifests on the specified cluster.
func (i *Installer) installManifests(
	cluster *shipper.Cluster,
	client kubernetes.Interface,
	restConfig *rest.Config,
	dynamicClientBuilderFunc DynamicClientBuilderFunc,
	manifests []string,
) error {

	it := i.InstallationTarget

	var configMap *corev1.ConfigMap
	var createdConfigMap *corev1.ConfigMap
	var existingConfigMap *corev1.ConfigMap
	var err error

	if configMap, err = janitor.CreateConfigMapAnchor(it); err != nil {
		return err
	} else if existingConfigMap, err = client.CoreV1().ConfigMaps(it.Namespace).Get(configMap.Name, metav1.GetOptions{}); err != nil && !errors.IsNotFound(err) {
		return shippererrors.NewKubeclientGetError(it.Name, configMap.Name, err).
			WithCoreV1Kind("ConfigMap")
	} else if err != nil { // errors.IsNotFound(err) == true
		if createdConfigMap, err = client.CoreV1().ConfigMaps(configMap.Namespace).Create(configMap); err != nil {
			return shippererrors.NewKubeclientCreateError(configMap, err).
				WithCoreV1Kind("ConfigMap")
		}
	} else {
		createdConfigMap = existingConfigMap
	}

	// Create the OwnerReference for the manifest objects.
	ownerReference := janitor.ConfigMapAnchorToOwnerReference(createdConfigMap)

	// We keep decoded objects and labels separately in order to perform
	// some intermediate checks and decorate labels if needed before the
	// actual patching happens.
	preparedObjects := make([]struct {
		decoded runtime.Object
		labels  map[string]string
	}, 0, len(manifests))

	var (
		productionLoadBalancerServices []*corev1.Service
		allServices                    []*corev1.Service
	)

	// Try to install all the rendered objects in the target cluster. We should
	// fail in the first error to report that this cluster has an issue. Since the
	// InstallationTarget.Status represent a per cluster status with a scalar
	// value, we don't try to install other objects for now.
	//
	// We'll do this in two parts: the first for loop will decode the manifest
	// and convert it to unstructured in addition of keep tabs of the number of
	// v1.Service manifests that have the lb label set to production.
	for _, manifest := range manifests {
		decodedObj, _, err :=
			kubescheme.Codecs.
				UniversalDeserializer().
				Decode([]byte(manifest), nil, nil)

		if err != nil {
			return shippererrors.NewDecodeManifestError("error decoding manifest: %s", err)
		}

		// We need the Deployment in the chart to have a unique name,
		// meaning that different installations need to generate
		// Deployments with different names, otherwise, we try to
		// overwrite a previous Deployment, and that fails with a
		// "field is immutable" error.
		if deployment, ok := decodedObj.(*appsv1.Deployment); ok {
			deploymentName := deployment.ObjectMeta.Name
			expectedName := it.GetName()
			if !strings.Contains(deploymentName, expectedName) {
				return shippererrors.NewInvalidChartError(
					fmt.Sprintf("Deployment %q has invalid name."+
						" The name of the Deployment should be"+
						" templated with {{.Release.Name}}.",
						deploymentName),
				)
			}
		}

		// Here we keep a counter of Services that have the lb label. This will
		// be used later on to determine whether or not an invalid error should
		// be returned to the caller.
		if svc, ok := decodedObj.(*corev1.Service); ok {
			allServices = append(allServices, svc)
			// Looking for a Service marked as the production LB
			if lbValue, ok := svc.Labels[shipper.LBLabel]; ok && lbValue == shipper.LBForProduction {
				// If we have already seen a service marked as a prod LB, it's an error
				if len(productionLoadBalancerServices) > 0 {
					return shippererrors.NewInvalidChartError(
						fmt.Sprintf("Object %#v contains %q label, but %#v claims"+
							" it is the production LB. This looks like a misconfig:"+
							" only 1 service is allowed to be the production LB.",
							decodedObj, shipper.LBLabel, productionLoadBalancerServices[0]))
				}
				productionLoadBalancerServices = append(productionLoadBalancerServices, svc)
			}
		}

		labels := mergeLabels(it.Labels, map[string]string{
			shipper.InstallationTargetOwnerLabel: it.Name,
		})

		preparedObjects = append(preparedObjects, struct {
			decoded runtime.Object
			labels  map[string]string
		}{decoded: decodedObj, labels: labels})
	}

	// If we have observed only 1 Service object and it was not marked
	// with shipper-lb=production label, we can do it ourselves.
	if len(productionLoadBalancerServices) == 0 && len(allServices) == 1 {
		productionLoadBalancerServices = allServices
	}

	// If, after all, we still can not identify a single Service which will
	// be the production LB, there is nothing else to do rather than bail out
	if len(productionLoadBalancerServices) != 1 {
		return shippererrors.NewInvalidChartError(
			fmt.Sprintf(
				"one and only one v1.Service object with label %q is required, but %d found instead",
				shipper.LBLabel, len(productionLoadBalancerServices)))
	}

	chosenService := productionLoadBalancerServices[0]
	if chosenService.Labels == nil {
		chosenService.Labels = make(map[string]string)
	}
	chosenService.Labels[shipper.LBLabel] = shipper.LBForProduction

	// The second loop is meant to install all the decoded and transformed
	// manifests once we assume it the Chart is in good shape.
	for _, r := range preparedObjects {
		decodedObj, err := i.patchObject(r.decoded, r.labels, &ownerReference)
		if err != nil {
			return err
		}

		// This is the Service object we picked as the production LB
		if decodedObj == chosenService {
			if svc, ok := decodedObj.(*corev1.Service); ok {
				decodedObj, err = i.modifyServiceSelector(svc)
				if err != nil {
					return err
				}
			} else {
				// This is a weird situation and this check is kept
				// here mostly for the sake of checking the world sanity
				return shippererrors.NewInvalidChartError(
					fmt.Sprintf("Object %#v is expected to be a Service."+
						" Can not proceed forward", decodedObj))
			}
		}

		// ResourceClient.Create() requires an Unstructured object to work with, so we
		// need to convert.
		unstrObj := &unstructured.Unstructured{}
		err = i.Scheme.Convert(decodedObj, unstrObj, nil)
		if err != nil {
			return shippererrors.NewConvertUnstructuredError("error converting object to unstructured: %s", err)
		}

		name := unstrObj.GetName()
		namespace := unstrObj.GetNamespace()
		gvk := unstrObj.GroupVersionKind()

		// Once we've gathered enough information about the document we want to
		// install, we're able to build a resource client to interact with the target
		// cluster.
		resourceClient, err := i.buildResourceClient(cluster, client, restConfig, dynamicClientBuilderFunc, &gvk)
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
			_, err = resourceClient.Create(unstrObj, metav1.CreateOptions{})
			if err != nil {
				return shippererrors.
					NewKubeclientCreateError(unstrObj, err).
					WithKind(gvk)
			}
			continue
		}

		// We inject a Namespace object in the objects to be installed
		// for a particular InstallationTarget; we don't want to
		// continue if the Namespace already exists.
		if gvk := existingObj.GroupVersionKind(); gvk.Kind == "Namespace" {
			continue
		}

		owner, ok := existingObj.GetLabels()[shipper.InstallationTargetOwnerLabel]
		if !ok {
			return shippererrors.NewMissingInstallationTargetOwnerLabelError(existingObj)
		}

		// If the existing object is owned by the installation target,
		// it doesn't need any installation, and we don't want to
		// updated because reasons.
		//
		// If it's owned by a different installation target, it'll be
		// overwritten only if it.Spec.CanOverride == true.
		if owner == it.Name || !it.Spec.CanOverride {
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

		existingObj.SetLabels(unstrObj.GetLabels())
		existingObj.SetAnnotations(unstrObj.GetAnnotations())
		existingUnstructuredObj := existingObj.UnstructuredContent()
		newUnstructuredObj := unstrObj.UnstructuredContent()
		switch decodedObj.(type) {
		case *corev1.Service:
			// Copy over clusterIP from existing object's .spec to the
			// rendered one.
			if clusterIP, ok, err := unstructured.NestedString(existingUnstructuredObj, "spec", "clusterIP"); ok {
				if err != nil {
					return err
				}

				unstructured.SetNestedField(newUnstructuredObj, clusterIP, "spec", "clusterIP")
			}
		}
		unstructured.SetNestedField(existingUnstructuredObj, newUnstructuredObj["spec"], "spec")
		existingObj.SetUnstructuredContent(existingUnstructuredObj)
		if _, clientErr := resourceClient.Update(existingObj, metav1.UpdateOptions{}); clientErr != nil {
			return shippererrors.
				NewKubeclientUpdateError(unstrObj, err).
				WithKind(gvk)
		}
	}

	return nil
}

// install attempts to install an InstallationTarget on the given cluster.
func (i *Installer) install(
	cluster *shipper.Cluster,
	client kubernetes.Interface,
	restConfig *rest.Config,
	dynamicClientBuilder DynamicClientBuilderFunc,
) error {
	renderedManifests, err := i.renderManifests(cluster)
	if err != nil {
		return err
	}

	return i.installManifests(cluster, client, restConfig, dynamicClientBuilder, renderedManifests)
}

// mergeLabels takes to sets of labels and merge them into another set.
//
// Values of the second set overwrite values from the first one.
func mergeLabels(a map[string]string, b map[string]string) map[string]string {

	labels := make(map[string]string)

	for k, v := range a {
		labels[k] = v
	}

	for k, v := range b {
		labels[k] = v
	}

	return labels
}
