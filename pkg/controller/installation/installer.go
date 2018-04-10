package installation

import (
	"fmt"

	"github.com/golang/glog"

	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperChart "github.com/bookingcom/shipper/pkg/chart"
	"github.com/bookingcom/shipper/pkg/label"
)

type DynamicClientBuilderFunc func(gvk *schema.GroupVersionKind, restConfig *rest.Config) dynamic.Interface

// Installer is an object that knows how to install Helm charts directly
// into Kubernetes clusters.
type Installer struct {
	fetchChart shipperChart.FetchFunc

	Release *shipperV1.Release
	Scheme  *runtime.Scheme
}

// NewInstaller returns a new Installer.
func NewInstaller(chartFetchFunc shipperChart.FetchFunc, release *shipperV1.Release) *Installer {
	return &Installer{
		fetchChart: chartFetchFunc,
		Release:    release,
		Scheme:     kubescheme.Scheme,
	}
}

// renderManifests returns a list of rendered manifests for the given release and
// cluster, or an error.
func (i *Installer) renderManifests(_ *shipperV1.Cluster) ([]string, error) {
	rel := i.Release
	chart, err := i.fetchChart(rel.Environment.Chart)
	if err != nil {
		return nil, err
	}

	rendered, err := shipperChart.Render(
		chart,
		rel.GetName(),
		rel.GetNamespace(),
		rel.Environment.Values,
	)

	for _, v := range rendered {
		glog.V(10).Infof("Rendered object:\n%s", v)
	}

	return rendered, err
}

// buildResourceClient returns a ResourceClient suitable to manipulate the kind
// of resource represented by the given GroupVersionKind at the given Cluster.
func (i *Installer) buildResourceClient(
	cluster *shipperV1.Cluster,
	client kubernetes.Interface,
	restConfig *rest.Config,
	dynamicClientBuilder DynamicClientBuilderFunc,
	gvk *schema.GroupVersionKind,
) (dynamic.ResourceInterface, error) {
	dynamicClient := dynamicClientBuilder(gvk, restConfig)

	// From the list of resources the target cluster knows about, find the resource for the
	// kind of object we have at hand.
	var resource *metaV1.APIResource
	gv := gvk.GroupVersion().String()
	if resources, err := client.Discovery().ServerResourcesForGroupVersion(gv); err != nil {
		return nil, err
	} else {
		for _, e := range resources.APIResources {
			if e.Kind == gvk.Kind {
				resource = &e
				break
			}
		}
		if resource == nil {
			return nil, fmt.Errorf("resource %s not found", gvk.Kind)
		}
	}

	// If it gets into this point, it means we have a resource, so we can create
	// a client for it scoping to the application's namespace. The namespace can
	// be ignored if creating, for example, objects that aren't bound to a
	// namespace.
	resourceClient := dynamicClient.Resource(resource, i.Release.Namespace)
	return resourceClient, nil
}

func (i *Installer) patchDeployment(d *appsV1.Deployment, labelsToInject map[string]string) runtime.Object {

	replicas := int32(0)
	d.Spec.Replicas = &replicas

	// patch .metadata.labels
	newLabels := d.Labels
	for k, v := range labelsToInject {
		newLabels[k] = v
	}
	d.SetLabels(newLabels)

	// Patch .spec.selector
	var newSelector *metaV1.LabelSelector
	if d.Spec.Selector != nil {
		newSelector = d.Spec.Selector.DeepCopy()
	} else {
		newSelector = &metaV1.LabelSelector{
			MatchLabels: map[string]string{},
		}
	}

	for k, v := range labelsToInject {
		newSelector.MatchLabels[k] = v
	}
	d.Spec.Selector = newSelector

	// Patch .spec.template.metadata.labels
	podTemplateLabels := d.Spec.Template.Labels
	for k, v := range labelsToInject {
		podTemplateLabels[k] = v
	}
	d.Spec.Template.SetLabels(podTemplateLabels)

	return d
}

func (i *Installer) patchService(s *coreV1.Service, labelsToInject map[string]string) runtime.Object {

	// We have a contract with the chart that they create one stably-named
	// (that is, the name should remain the same from release to release)
	// Service per application with a special label: 'shipper-lb: production'.
	// We don't want to add the 'release' label to this service because the
	// lifetime spans releases: this is an application-scoped object, not
	// a release-scoped object. Other services are not part of this
	// contract; user charts can do whatever they want.
	lbType, ok := s.GetLabels()[shipperV1.LBLabel]
	if ok && lbType == shipperV1.LBForProduction {
		labelsToInject = label.FilterRelease(labelsToInject)
	}

	labels := s.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	for k, v := range labelsToInject {
		labels[k] = v
	}

	s.SetLabels(labels)
	return s
}

func (i *Installer) patchUnstructured(o *unstructured.Unstructured, labelsToInject map[string]string) runtime.Object {
	labels := o.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	for k, v := range labelsToInject {
		labels[k] = v
	}
	o.SetLabels(labels)
	return o
}

func (i *Installer) patchObject(object runtime.Object, labelsToInject map[string]string) runtime.Object {
	switch o := object.(type) {
	case *appsV1.Deployment:
		return i.patchDeployment(o, labelsToInject)
	case *coreV1.Service:
		return i.patchService(o, labelsToInject)
	default:
		unstructuredObj := &unstructured.Unstructured{}
		err := i.Scheme.Convert(object, unstructuredObj, nil)
		if err != nil {
			panic(err)
		}
		return i.patchUnstructured(unstructuredObj, labelsToInject)
	}
}

// installManifests attempts to install the manifests on the specified cluster.
func (i *Installer) installManifests(
	cluster *shipperV1.Cluster,
	client kubernetes.Interface,
	restConfig *rest.Config,
	dynamicClientBuilderFunc DynamicClientBuilderFunc,
	manifests []string,
) error {

	// Try to install all the rendered objects in the target cluster. We should
	// fail in the first error to report that this cluster has an issue. Since
	// the InstallationTarget.Status represent a per cluster status with a
	// scalar value, we don't try to install other objects for now.
	for _, manifest := range manifests {

		// This one was tricky to find out. @asurikov pointed me out to the
		// UniversalDeserializer, which can decode a []byte representing the
		// k8s manifest into the proper k8s object (for example, v1.Service).
		// Haven't tested the decoder with CRDs, so please keep a mental note
		// that it might not work as expected (meaning more research might be
		// necessary).
		decodedObj, gvk, err :=
			kubescheme.Codecs.UniversalDeserializer().Decode([]byte(manifest), nil, nil)
		if err != nil {
			return fmt.Errorf("error decoding manifest: %s", err)
		}

		// We label final objects with Release labels so that we can find/filter them
		// later in Capacity and Installation controllers.
		// This may overwrite some of the pre-existing labels. It's not ideal but with
		// current implementation we require that shipperv1.ReleaseLabel is propagated
		// correctly. This may be subject to change.
		labelsToInject := i.Release.Labels
		decodedObj = i.patchObject(decodedObj, labelsToInject)

		// ResourceClient.Create() requires an Unstructured object to work with, so
		// we need to convert from v1.Service into a map[string]interface{}, which
		// is what ToUnstrucured() below does. To find this one, I had to find a
		// Merge Request then track the git history to find out where it was moved
		// to, since there's no documentation whatsoever about it anywhere.
		obj := &unstructured.Unstructured{}
		err = i.Scheme.Convert(decodedObj, obj, nil)
		if err != nil {
			return fmt.Errorf("error converting object to unstructured: %s", err)
		}

		// Once we've gathered enough information about the document we want to install,
		// we're able to build a resource client to interact with the target cluster.
		resourceClient, err := i.buildResourceClient(cluster, client, restConfig, dynamicClientBuilderFunc, gvk)
		if err != nil {
			return fmt.Errorf("error building resource client: %s", err)
		}

		// Now we can create the object using the resource client. Probably all of
		// the business logic from decodeManifest() until resourceClient.create() could
		// be abstracted into a method.
		_, err = resourceClient.Create(obj)
		if err != nil {

			// What sort of heuristics should we use to assume that an object
			// has already been created *and* it is the right object? If the right
			// object is already created, then we should continue. For now we will
			// naively assume that if a file with the expected name exists, it was
			// created by us.
			if errors.IsAlreadyExists(err) {
				continue
			}

			// Perhaps we want to annotate differently the error when the request
			// couldn't be constructed? Can be removed later on if not proven useful.
			if rce, ok := err.(*rest.RequestConstructionError); ok {
				return fmt.Errorf("error constructing request: %s", rce)
			}

			return fmt.Errorf(`error creating resource %s "%s/%s": %s`, obj.GetKind(), obj.GetNamespace(), obj.GetName(), err)
		}
	}

	return nil
}

// installRelease attempts to install the given release on the given cluster.
func (i *Installer) installRelease(
	cluster *shipperV1.Cluster,
	client kubernetes.Interface,
	restConfig *rest.Config,
	dynamicClientBuilder DynamicClientBuilderFunc,
) error {

	renderedManifests, err := i.renderManifests(cluster)
	if err != nil {
		return err
	}

	renderedManifests = injectNamespace(i.Release.Namespace, renderedManifests)

	return i.installManifests(cluster, client, restConfig, dynamicClientBuilder, renderedManifests)
}

// injectNamespace prepends a rendered v1/Namespace called name to manifests and
// returns the resulting slice. Should be called before manifests are installed.
func injectNamespace(name string, manifests []string) []string {
	const ns = `
apiVersion: v1
kind: Namespace
metadata:
  name: %q
`

	withNs := make([]string, len(manifests)+1)
	withNs[0] = fmt.Sprintf(ns, name)
	copy(withNs[1:], manifests)

	return withNs
}
