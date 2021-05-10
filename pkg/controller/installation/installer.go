package installation

import (
	"fmt"
	"reflect"
	"sort"

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
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	"github.com/bookingcom/shipper/pkg/util/anchor"
)

type DynamicClientBuilderFunc func(gvk *schema.GroupVersionKind, restConfig *rest.Config, cluster *shipper.Cluster) dynamic.Interface

// Installer is an object that knows how to install objects into Kubernetes
// clusters.
type Installer struct {
	installationTarget *shipper.InstallationTarget
	objects            []runtime.Object
}

// NewInstaller returns a new Installer.
func NewInstaller(
	it *shipper.InstallationTarget,
	objects []runtime.Object,
) *Installer {
	return &Installer{
		installationTarget: it,
		objects:            objects,
	}
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
			klog.Warningf(
				"failed to create anchor ConfigMap %s/%s in cluster %s: %+v",
				configMap.Namespace,
				configMap.Name,
				cluster.Name,
				err,
				)
			return shippererrors.NewKubeclientCreateError(configMap, err).
				WithCoreV1Kind("ConfigMap")
		}
	} else {
		createdConfigMap = existingConfigMap
	}

	ownerReference := anchor.ConfigMapAnchorToOwnerReference(createdConfigMap)
	resourceClients := make(map[string]dynamic.ResourceInterface)

	for _, preparedObj := range i.objects {
		obj := &unstructured.Unstructured{}
		err = kubescheme.Scheme.Convert(preparedObj, obj, nil)
		if err != nil {
			return shippererrors.NewConvertUnstructuredError("error converting object to unstructured: %s", err)
		}

		name := obj.GetName()
		namespace := obj.GetNamespace()
		gvk := obj.GroupVersionKind()

		resourceClient, ok := resourceClients[gvk.String()]
		if !ok {
			var err error
			resourceClient, err = i.buildResourceClient(
				cluster,
				client,
				restConfig,
				dynamicClientBuilderFunc,
				&gvk,
			)
			if err != nil {
				return err
			}

			resourceClients[gvk.String()] = resourceClient
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
