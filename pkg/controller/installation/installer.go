package installation

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/golang/glog"
	"github.com/bookingcom/shipper/pkg/controller/janitor"

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
)

type DynamicClientBuilderFunc func(gvk *schema.GroupVersionKind, restConfig *rest.Config) dynamic.Interface

// Installer is an object that knows how to install Helm charts directly
// into Kubernetes clusters.
type Installer struct {
	fetchChart shipperChart.FetchFunc

	Release            *shipperV1.Release
	InstallationTarget *shipperV1.InstallationTarget
	Scheme             *runtime.Scheme
}

// NewInstaller returns a new Installer.
func NewInstaller(chartFetchFunc shipperChart.FetchFunc,
	release *shipperV1.Release,
	it *shipperV1.InstallationTarget,
) *Installer {
	return &Installer{
		fetchChart:         chartFetchFunc,
		Release:            release,
		InstallationTarget: it,
		Scheme:             kubescheme.Scheme,
	}
}

// renderManifests returns a list of rendered manifests for the given release and
// cluster, or an error.
func (i *Installer) renderManifests(_ *shipperV1.Cluster) ([]string, error) {
	rel := i.Release
	chart, err := i.fetchChart(rel.Environment.Chart)
	if err != nil {
		return nil, RenderManifestError(err)
	}

	rendered, err := shipperChart.Render(
		chart,
		rel.GetName(),
		rel.GetNamespace(),
		rel.Environment.Values,
	)

	if err != nil {
		err = RenderManifestError(err)
	}

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

func (i *Installer) patchDeployment(
	d *appsV1.Deployment,
	labelsToInject map[string]string,
	ownerReference *metaV1.OwnerReference,
) runtime.Object {

	d.OwnerReferences = []metaV1.OwnerReference{*ownerReference}

	replicas := int32(0)
	d.Spec.Replicas = &replicas

	// patch .metadata.labels
	newLabels := d.Labels
	if newLabels == nil {
		newLabels = map[string]string{}
	}

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

func (i *Installer) patchService(
	s *coreV1.Service,
	labelsToInject map[string]string,
	ownerReference *metaV1.OwnerReference,
) runtime.Object {
	s.SetOwnerReferences([]metaV1.OwnerReference{*ownerReference})
	s.SetLabels(mergeLabels(s.GetLabels(), labelsToInject))
	return s
}

func (i *Installer) patchUnstructured(
	o *unstructured.Unstructured,
	labelsToInject map[string]string,
	ownerReference *metaV1.OwnerReference,
) runtime.Object {

	o.SetOwnerReferences([]metaV1.OwnerReference{*ownerReference})

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

func (i *Installer) patchObject(
	object runtime.Object,
	labelsToInject map[string]string,
	ownerReference *metaV1.OwnerReference,
) runtime.Object {
	switch o := object.(type) {
	case *appsV1.Deployment:
		return i.patchDeployment(o, labelsToInject, ownerReference)
	case *coreV1.Service:
		return i.patchService(o, labelsToInject, ownerReference)
	default:
		unstructuredObj := &unstructured.Unstructured{}
		err := i.Scheme.Convert(object, unstructuredObj, nil)
		if err != nil {
			panic(err)
		}
		return i.patchUnstructured(unstructuredObj, labelsToInject, ownerReference)
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

	var createdConfigMap *coreV1.ConfigMap

	// Create a ConfigMap to act as the Deployment owner.
	if configMap, err := janitor.CreateConfigMapAnchor(i.InstallationTarget); err != nil {
		return NewCreateResourceError("error creating anchor config map: %s ", err)
	} else if createdConfigMap, err = client.CoreV1().ConfigMaps(configMap.Namespace).Create(configMap); err != nil {
		// If the anchor exists in the cluster, then it is likely that a failure
		// after its creation has happened. We should not exit just yet.
		if errors.IsAlreadyExists(err) {
			createdConfigMap, err = client.CoreV1().ConfigMaps(i.Release.Namespace).Get(configMap.Name, metaV1.GetOptions{})
			if err != nil {
				return NewCreateResourceError(
					`error getting anchor %q: %s`,
					configMap.Name, err)
			}
		} else {
			return NewCreateResourceError(
				`error creating resource %s "%s/%s": %s`,
				configMap.Kind, configMap.Namespace, configMap.Name, err)
		}
	}

	// Create the OwnerReference for the manifest objects
	ownerReference := janitor.ConfigMapAnchorToOwnerReference(createdConfigMap)

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
			kubescheme.Codecs.
				UniversalDeserializer().
				Decode([]byte(manifest), nil, nil)

		if err != nil {
			return NewDecodeManifestError("error decoding manifest: %s", err)
		}

		// We label final objects with Release labels so that we can find/filter them
		// later in Capacity and Installation controllers.
		// This may overwrite some of the pre-existing labels. It's not ideal but with
		// current implementation we require that shipperv1.ReleaseLabel is propagated
		// correctly. This may be subject to change.
		labelsToInject := i.Release.Labels
		decodedObj = i.patchObject(decodedObj, labelsToInject, &ownerReference)

		// ResourceClient.Create() requires an Unstructured object to work with, so
		// we need to convert from v1.Service into a map[string]interface{}, which
		// is what ToUnstrucured() below does. To find this one, I had to find a
		// Merge Request then track the git history to find out where it was moved
		// to, since there's no documentation whatsoever about it anywhere.
		obj := &unstructured.Unstructured{}
		err = i.Scheme.Convert(decodedObj, obj, nil)
		if err != nil {
			return NewConvertUnstructuredError("error converting object to unstructured: %s", err)
		}

		// Once we've gathered enough information about the document we want to install,
		// we're able to build a resource client to interact with the target cluster.
		resourceClient, err := i.buildResourceClient(cluster, client, restConfig, dynamicClientBuilderFunc, gvk)
		if err != nil {
			return NewResourceClientError("error building resource client: %s", err)
		}

		// Now we can create the object using the resource client. Probably all of
		// the business logic from decodeManifest() until resourceClient.create() could
		// be abstracted into a method.
		_, err = resourceClient.Create(obj)
		if err != nil {

			// In the case an object with the same name and gvk already exists
			// in the server, we then explicitly overwrite the object's
			// .metadata and .spec fields and update the faulty object.
			if errors.IsAlreadyExists(err) {

				// We must first retrieve the object from the faulty cluster.
				// It returns to the caller here with the error since there's
				// nothing we can do to recover.
				existingObj, clientErr := resourceClient.Get(obj.GetName(), metaV1.GetOptions{ResourceVersion: obj.GetResourceVersion()})
				if clientErr != nil {
					return NewGetResourceError(`error retrieving resource %s "%s/%s": %s`,
						obj.GetKind(), obj.GetNamespace(), obj.GetName(), clientErr)
				}

				if gvk := existingObj.GroupVersionKind(); gvk.Kind == "Namespace" {
					// Shipper tries to add the application's Namespace if
					// missing, so it doesn't have the ReleaseLabel and the
					// next check will fail anyways.
					continue
				}

				if releaseLabelValue, ok := existingObj.GetLabels()[shipperV1.ReleaseLabel]; !ok {
					// TODO(isutton): Do we want to fix this situation or bail out?
					return fmt.Errorf("Object misses release label")
				} else if releaseLabelValue == i.Release.GetName() {
					// This Release was the last one to either touch or
					// create this object; nothing to do for the current
					// manifest.
					continue
				}

				// Now we start with the bad: we want to overwrite the existing
				// object with the contents of the rendered manifest. To do so,
				// we need to preserve TypeMeta and ObjectMeta almost entirely,
				// with exception of (at the time of this writing) annotations
				// and labels, since the other fields can't be changed. Probably
				// we'll be able to remove this once the server side equivalent
				// of 'kubectl apply' gets implemented in Kubernetes.

				ownerReferenceFound := false
				for _, o := range existingObj.GetOwnerReferences() {
					if reflect.DeepEqual(o, ownerReference) {
						// Proceed to the next manifest if this release's anchor
						// is already present in the list of owner references --
						// meaning the faulty object has already been touched.
						ownerReferenceFound = true
					}
				}

				// Add a new owner reference to the existing object's owner
				// references, since we need to keep the existing ones. An
				// object can be owned by several release anchor objects; those
				// objects will be removed from the cluster once all the anchors
				// have been removed.
				if !ownerReferenceFound {
					ownerReferences := append(existingObj.GetOwnerReferences(), ownerReference)
					sort.Slice(ownerReferences, func(i, j int) bool {
						return ownerReferences[i].Name < ownerReferences[j].Name
					})
					existingObj.SetOwnerReferences(ownerReferences)
				}

				// Override labels and annotations in the existing object using
				// the values extracted from the rendered manifests. We started
				// with the idea of overriding `.metadata` but the rendered
				// manifest's isn't rich enough for an update, since it lacks all
				// the read-only fields from ObjectMeta and TypeMeta.
				existingObj.SetLabels(obj.GetLabels())
				existingObj.SetAnnotations(obj.GetAnnotations())

				// Now we start poking into the unstructured content of the
				// existing object, trying to overwrite the existing data as
				// much as possible.
				existingUnstructuredObj := existingObj.UnstructuredContent()
				newUnstructuredObj := obj.UnstructuredContent()

				// Not, this is the ugly: before we put our fingers on its guts,
				// we need to gather some data that we know will generate an
				// error when "applying" the changes on some objects, for example
				// Service: if the .spec.clusterIP field is rendered empty, the
				// API server will return an error in eventual "upserts". This is
				// the reason that we have this type switch below.
				switch decodedObj.(type) {
				case *coreV1.Service:
					// Copy over clusterIP from existing object's .spec to the
					// rendered one.
					if clusterIP, ok := unstructured.NestedString(existingUnstructuredObj, "spec", "clusterIP"); ok {
						unstructured.SetNestedField(newUnstructuredObj, clusterIP, "spec", "clusterIP")
					}
				}

				// Now we need to go down to the unstructured content and copy
				// the rendered object's .spec to the existing object's.
				unstructured.SetNestedField(existingUnstructuredObj, newUnstructuredObj["spec"], "spec")

				// I believe this is not required, since UnstructuredContent()
				// method returns a map but I'm being overcautious since it could
				// instead return a copy at some point in the future and
				// we wouldn't notice until we place a breakpoint in here.
				existingObj.SetUnstructuredContent(existingUnstructuredObj)

				// And finally we try to update the faulty object with the
				// overwritten fields. If an error happens here, return an error
				// since there's nothing we can do about anymore.
				if _, clientErr = resourceClient.Update(existingObj); clientErr != nil {
					return NewUpdateResourceError(`error updating resource %s "%s/%s": %s`,
						existingObj.GetKind(), obj.GetNamespace(), obj.GetName(), clientErr)
				}

				// Continue processing the next rendered manifest if everything
				// worked so far.
				continue
			}

			// Perhaps we want to annotate differently the error when the request
			// couldn't be constructed? Can be removed later on if not proven useful.
			if rce, ok := err.(*rest.RequestConstructionError); ok {
				return fmt.Errorf("error constructing request: %s", rce)
			}

			return NewCreateResourceError(
				`error creating resource %s "%s/%s": %s`,
				obj.GetKind(), obj.GetNamespace(), obj.GetName(), err)
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
