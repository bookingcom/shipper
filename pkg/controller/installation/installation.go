package installation

import (
	"fmt"
	"github.com/golang/glog"
	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperChart "github.com/bookingcom/shipper/pkg/chart"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"os/user"
	"path"
)

// renderManifests returns a list of rendered manifests for the given release and
// cluster, or an error.
func (c *Controller) renderManifests(cluster *shipperV1.Cluster, release *shipperV1.Release) ([]string, error) {
	options := release.Options(cluster)
	chrt, err := release.Chart()
	if err != nil {
		return nil, err
	}
	vals, err := release.Values()
	if err != nil {
		return nil, err
	}
	return shipperChart.RenderChart(chrt, vals, options)
}

// buildConfig returns a configuration suited to communicate with the given cluster.
func (c *Controller) buildConfig(cluster *shipperV1.Cluster, gvk *schema.GroupVersionKind) (*rest.Config, error) {

	// Set up the initial client configuration
	cfg := &rest.Config{
		Host:          cluster.Spec.APIMaster,
		APIPath:       dynamic.LegacyAPIPathResolverFunc(*gvk),
		ContentConfig: dynamic.ContentConfig(),
	}

	// We need to update the configuration's GroupVersion with the information
	// found in GroupVersionKind. This is required otherwise the ResourceClient
	// won't be able to compute the right URL to interact with the target cluster.
	cfg.GroupVersion = &schema.GroupVersion{Group: gvk.Group, Version: gvk.Version}

	// The following configuration is meant only to be used with minikube. In
	// production it is likely that we'll need to provide some sort of
	// configuration regarding Cluster secrets. One idea is to have a secret
	// with the same interface rest.Config accepts and use it to compose the
	// cluster's client configuration.
	if cluster.Name == "minikube" {
		usr, _ := user.Current()
		cfg.CAFile = path.Join(usr.HomeDir, ".minikube", "ca.crt")
		cfg.CertFile = path.Join(usr.HomeDir, ".minikube", "client.crt")
		cfg.KeyFile = path.Join(usr.HomeDir, ".minikube", "client.key")
	}

	return cfg, nil
}

// decodeManifest attempts to deserialize the provided manifest. It returns
// an unstructured decoded object, suitable to be used with a ResourceClient
// and the object's GroupVersionKind, or an error.
func decodeManifest(manifest string) (*unstructured.Unstructured, *schema.GroupVersionKind, error) {

	// This one was tricky to find out. @asurikov pointed me out to the
	// UniversalDeserializer, which can decode a []byte representing the
	// k8s manifest into the proper k8s object (for example, v1.Service).
	// Haven't tested the decoder with CRDs, so please keep a mental note
	// that it might not work as expected (meaning more research might be
	// necessary).
	decodedObj, gvk, err := scheme.Codecs.UniversalDeserializer().Decode([]byte(manifest), nil, nil)
	if err != nil {
		return nil, nil, err
	}

	// ResourceClient.Create() requires an Unstructured object to work with, so
	// we need to convert from v1.Service into a map[string]interface{}, which
	// is what ToUnstrucured() below does. To find this one, I had to find a
	// Merge Request then track the git history to find out where it was moved
	// to, since there's no documentation whatsoever about it anywhere.
	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(decodedObj)
	if err != nil {
		return nil, nil, err
	}

	return &unstructured.Unstructured{unstructuredObj}, gvk, nil
}

// discoverResource returns an APIResource for a group, version and kind.
func discoverResource(client *kubernetes.Clientset, gvk *schema.GroupVersionKind) (*v1.APIResource, error) {

	gv := gvk.GroupVersion().String()
	resources, err := client.Discovery().ServerResourcesForGroupVersion(gv)
	if err != nil {
		return nil, err
	}

	var resource *v1.APIResource
	for _, e := range resources.APIResources {
		if e.Kind == gvk.Kind {
			resource = &e
			break
		}
	}

	if resource == nil {
		return nil, fmt.Errorf("resource %s not found", gvk.Kind)
	}

	return resource, nil
}

// buildResourceClient returns a ResourceClient suitable to communicate with
// the given cluster, or an error if there are not resources matching the given
// GroupVersionKind.
func (c *Controller) buildResourceClient(
	cluster *shipperV1.Cluster,
	gvk *schema.GroupVersionKind,
	ns string,
) (dynamic.ResourceInterface, error) {

	cfg, err := c.buildConfig(cluster, gvk)
	if err != nil {
		return nil, err
	}

	dynamicClient, err := dynamic.NewClient(cfg)
	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	resource, err := discoverResource(client, gvk)
	if err != nil {
		return nil, err
	}

	resourceClient := dynamicClient.Resource(resource, ns)
	return resourceClient, nil
}

// installManifests attempts to install the manifests on the specified cluster.
func (c *Controller) installManifests(
	cluster *shipperV1.Cluster,
	ns string,
	manifests []string,
) error {

	// Try to install all the rendered objects in the target cluster. We should
	// fail in the first error to report that this cluster has an issue. Since
	// the InstallationTarget.Status represent a per cluster status with a
	// scalar value, we don't try to install other objects for now.
	for _, manifest := range manifests {

		// Extract from the rendered object an unstructured representation of the object,
		// together with its GroupVersionKind that will be used to grab a ResourceClient
		// for this object.
		obj, gvk, err := decodeManifest(manifest)
		if err != nil {
			return err
		}

		// Once we've gathered enough information about the document we want to install,
		// we're able to build a resource client to interact with the target cluster.
		resourceClient, err := c.buildResourceClient(cluster, gvk, ns)
		if err != nil {
			return err
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

			// Log any other kind of errors.
			glog.Error(err)

			// Perhaps we want to annotate differently the error when the request
			// couldn't be constructed? Can be removed later on if not proven useful.
			if rce, ok := err.(*rest.RequestConstructionError); ok {
				return rce
			}

			return fmt.Errorf("other: %s", err)
		}
	}

	return nil
}

// installRelease attempts to install the given release on the given cluster.
func (c *Controller) installRelease(
	release *shipperV1.Release,
	cluster *shipperV1.Cluster,
) error {

	renderedManifests, err := c.renderManifests(cluster, release)
	if err != nil {
		return err
	}

	err = c.installManifests(cluster, release.Namespace, renderedManifests)
	if err != nil {
		return err
	}

	return nil
}

// processInstallation attempts to install the related release on all target clusters.
func (c *Controller) processInstallation(it *shipperV1.InstallationTarget) error {

	release, err := c.releaseLister.Releases(it.Namespace).Get(it.Name)
	if err != nil {
		glog.Error(err)
		return err
	}

	// The strategy here is try our best to install as many objects as possible
	// in all target clusters. It is not the Installation Controller job to
	// reason about a target cluster status.
	clusterStatuses := make([]shipperV1.ClusterInstallationStatus, 0)
	for _, name := range it.Spec.Clusters {
		status := shipperV1.ClusterInstallationStatus{Name: name}

		if cluster, err := c.clusterLister.Get(name); err != nil {
			status.Status = shipperV1.InstallationStatusFailed
			status.Message = err.Error()
		} else {
			if err = c.installRelease(release, cluster); err != nil {
				status.Status = shipperV1.InstallationStatusFailed
				status.Message = err.Error()
			} else {
				status.Status = shipperV1.InstallationStatusInstalled
			}
		}

		clusterStatuses = append(clusterStatuses, status)
	}

	it.Status.Clusters = clusterStatuses

	_, err = c.shipperclientset.ShipperV1().InstallationTargets(it.Namespace).Update(it)
	if err != nil {
		glog.Error(err)
		return err
	}

	return nil
}
