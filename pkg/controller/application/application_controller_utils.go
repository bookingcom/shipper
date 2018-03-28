package application

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strconv"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	metatypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	helmchart "k8s.io/helm/pkg/proto/hapi/chart"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperchart "github.com/bookingcom/shipper/pkg/chart"
)

// traverseSuccessors, given a namespace and an app name, finds the latest
// Release by following release.status.successor links.
// Returns an error if more than one Release is found. Returned Release can be
// nil even if there's no error (i.e. no installed Releases).
func (c *Controller) traverseSuccessors(ns string, appName string) (*shipperv1.Release, error) {
	selector := labels.Set{
		shipperv1.AppLabel: appName,
	}.AsSelector()

	releases, err := c.relLister.Releases(ns).List(selector)
	if err != nil {
		return nil, fmt.Errorf("find latest Release for %q in %q: %s", selector, ns, err)
	}

	if len(releases) == 0 {
		return nil, nil
	}

	releasesByUID := map[metatypes.UID]*shipperv1.Release{}
	seenPredecessors := map[metatypes.UID]bool{}
	var start *shipperv1.Release
	// find the single release with no predecessor: the start of the lineage
	for _, r := range releases {
		releasesByUID[r.GetUID()] = r
		if r.Status.Predecessor != nil {
			_, ok := seenPredecessors[r.Status.Predecessor.UID]
			// prevent branches
			// TODO(btyler) is this reasonable? do we _never_ have a branch of the tree?
			if ok {
				return nil, fmt.Errorf(
					"Found at least two releases for app '%s/%s' with the same predecessor, %q",
					ns, appName, r.Status.Predecessor.Name,
				)
			}
			seenPredecessors[r.Status.Predecessor.UID] = true
			continue
		}

		// prevent multiple independent graphs
		if start != nil {
			return nil, fmt.Errorf("Found at least two releases for app '%s/%s' without predecessors", ns, appName)
		}
		start = r
	}

	var head *shipperv1.Release
	current := start
	var i int
	// traverse foward through the releases following the lineage.
	// we expect a simple list: stopping at len(releases) + 1 gives us an
	// indicator if we got trapped in a loop
	for i = 0; i < len(releases)+1; i++ {
		if current.Status.Successor == nil {
			head = current
			break
		}
		successor, ok := releasesByUID[current.Status.Successor.UID]
		// I have a successor, but it isn't a known release: a dangling pointer
		if !ok {
			head = current
			break
		}
		current = successor
	}

	if i >= len(releases) {
		return nil, fmt.Errorf("Releases in namespace %q form a cyclic graph, not a linked list", ns)
	}

	return head, nil
}

func (c *Controller) hasInboundPredecessorPointer(appName string, release *shipperv1.Release) (bool, error) {
	selector := labels.Set{
		shipperv1.AppLabel: appName,
	}.AsSelector()

	releases, err := c.relLister.Releases(release.GetNamespace()).List(selector)
	if err != nil {
		return false, fmt.Errorf("hasInboundPredecessorPointer %q in %q: %s", selector, release.GetNamespace(), err)
	}

	for _, rel := range releases {
		if rel.Status.Predecessor != nil {
			if rel.Status.Predecessor.UID == release.GetUID() {
				return true, nil
			}
		}
	}

	return false, nil
}

func (c *Controller) createReleaseForApplication(app *shipperv1.Application) error {
	chart, err := c.fetchChart(app.Spec.Template.Chart)
	if err != nil {
		return fmt.Errorf("failed to fetch chart for Application %q: %s", metaKey(app), err)
	}

	replicas, err := extractReplicasFromChart(chart, app)
	if err != nil {
		return err
	}
	glog.V(6).Infof(`Extracted %+v replicas from Application %q`, replicas, metaKey(app))

	// label releases with their hash; select by that label and increment if needed
	// appname-hash-of-template-generation
	releaseName, generation, err := c.releaseNameForApplication(app)
	if err != nil {
		return err
	}
	releaseNs := app.GetNamespace()

	labels := make(map[string]string)
	for k, v := range app.GetLabels() {
		labels[k] = v
	}

	labels[shipperv1.ReleaseLabel] = releaseName
	labels[shipperv1.AppLabel] = app.GetName()
	labels[shipperv1.ReleaseEnvironmentHashLabel] = hashReleaseEnvironment(app.Spec.Template)

	annotations := map[string]string{
		shipperv1.ReleaseReplicasAnnotation:           strconv.Itoa(replicas),
		shipperv1.ReleaseTemplateGenerationAnnotation: strconv.Itoa(generation),
	}

	var (
		new  *shipperv1.Release
		pred *shipperv1.Release
	)

	new = &shipperv1.Release{
		ReleaseMeta: shipperv1.ReleaseMeta{
			ObjectMeta: metav1.ObjectMeta{
				Name:        releaseName,
				Namespace:   releaseNs,
				Labels:      labels,
				Annotations: annotations,
				OwnerReferences: []metav1.OwnerReference{
					createOwnerRefFromApplication(app),
				},
			},
			Environment: *(app.Spec.Template.DeepCopy()),
		},
		Spec: shipperv1.ReleaseSpec{},
		Status: shipperv1.ReleaseStatus{
			Phase: shipperv1.ReleasePhaseWaitingForScheduling,
		},
	}

	pred, err = c.traverseSuccessors(releaseNs, app.GetName())
	if err != nil {
		return err
	}

	ri := c.shipperClientset.ShipperV1().Releases(releaseNs)

	// It's important that we do Update first. If the update fails, at least there
	// won't be two Releases without a successor. Recovering from this would
	// require reasoning about order of things e.g. based on timestamps. In a
	// distributed system this can be complicated.

	if pred != nil {
		glog.V(4).Infof("Release %q is the predecessor of %q", releaseName, pred.GetName())

		new.Status.Predecessor = &corev1.ObjectReference{
			APIVersion: pred.APIVersion,
			Kind:       pred.Kind,
			Name:       pred.GetName(),
			Namespace:  releaseNs,
		}

		pred.Status.Successor = &corev1.ObjectReference{
			APIVersion: new.APIVersion,
			Kind:       new.Kind,
			Name:       releaseName,
			Namespace:  releaseNs,
		}

		// TODO change to UpdateStatus when kubernetes#38113 is merged.
		// TODO Patch
		if _, err = ri.Update(pred); err != nil {
			// If Update failed, we bail out with pred untouched and new is not created.
			// No recovery needed.
			return fmt.Errorf("set successor for Release %q: %s", metaKey(pred), err)
		}
	} else {
		// Must be a first deployment of a new app.
		glog.V(4).Infof("No predecessor for Release %q", metaKey(new))
	}

	new, err = ri.Create(new)
	if err != nil {
		// If Update went through but Create failed, we have pred pointing to a
		// Release that does not exist. We can recover from this by fixing the
		// dangling successor pointer.
		// Requeue pred directly so that we don't need to wait a full re-sync period
		// before progress can be made.
		if pred != nil {
			c.relWorkqueue.AddRateLimited(metaKey(pred))
		}
		return fmt.Errorf("create Release for Application %q: %s", metaKey(app), err)
	}

	/*
		c.recorder.Eventf(
			new,
			corev1.EventTypeNormal,
			reasonShipping,
			"Created Release %q",
			releaseName,
		)
	*/

	return nil
}

func (c *Controller) releaseNameForApplication(app *shipperv1.Application) (string, int, error) {
	hash := hashReleaseEnvironment(app.Spec.Template)
	selector := labels.Set{
		shipperv1.AppLabel:                    app.GetName(),
		shipperv1.ReleaseEnvironmentHashLabel: hash,
	}.AsSelector()

	releases, err := c.relLister.Releases(app.GetNamespace()).List(selector)
	if err != nil {
		return "", 0, err
	}

	// no other releases with this template exist
	if len(releases) == 0 {
		return fmt.Sprintf("%s-%s-%d", app.GetName(), hash, 0), 0, nil
	}

	highestObserved := 0
	for _, rel := range releases {
		generationStr, ok := rel.GetAnnotations()[shipperv1.ReleaseTemplateGenerationAnnotation]
		if !ok {
			return "", 0, fmt.Errorf("Release %q does not have a generation annotation", metaKey(rel))
		}
		generation, err := strconv.Atoi(generationStr)
		if err != nil {
			return "", 0, fmt.Errorf("Release %q has an invalid generation (failed strconv): %v", metaKey(rel), generationStr)
		}

		if generation > highestObserved {
			highestObserved = generation
		}
	}

	newGeneration := highestObserved + 1
	return fmt.Sprintf("%s-%s-%d", app.GetName(), hash, newGeneration), newGeneration, nil
}

func extractReplicasFromChart(chart *helmchart.Chart, app *shipperv1.Application) (int, error) {
	rendered, err := shipperchart.Render(chart, app.ObjectMeta.Name, app.ObjectMeta.Namespace, app.Spec.Template.Values)
	if err != nil {
		return 0, fmt.Errorf("extract replicas for Application %q: %s", metaKey(app), err)
	}

	deployments := shipperchart.GetDeployments(rendered)
	if n := len(deployments); n != 1 {
		return 0, fmt.Errorf("extract replicas for Application %q: expected exactly one Deployment but got %d", metaKey(app), n)
	}

	replicas := deployments[0].Spec.Replicas
	// deployments default to 1 replica when replicas is nil or unspecified
	// see k8s.io/api/apps/v1/types.go DeploymentSpec
	if replicas == nil {
		return 1, nil
	}

	return int(*replicas), nil
}

func metaKey(obj runtime.Object) string {
	key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	return key
}

func identicalEnvironments(envs ...shipperv1.ReleaseEnvironment) bool {
	if len(envs) == 0 {
		return true
	}

	referenceHash := hashReleaseEnvironment(envs[0])
	for _, env := range envs {
		if referenceHash != hashReleaseEnvironment(env) {
			return false
		}
	}
	return true
}

func hashReleaseEnvironment(env shipperv1.ReleaseEnvironment) string {
	b, err := json.Marshal(env)
	if err != nil {
		// TODO(btyler) ???
		panic(err)
	}

	hash := fnv.New32a()
	hash.Write(b)
	return fmt.Sprintf("%x", hash.Sum32())
}

// the strings here are insane, but if you create a fresh release object for
// some reason it lands in the work queue with an empty TypeMeta. This is resolved
// if you restart the controllers, so I'm not sure what's going on.
// https://github.com/kubernetes/client-go/issues/60#issuecomment-281533822 and
// https://github.com/kubernetes/client-go/issues/60#issuecomment-281747911 give
// some potential context.
func createOwnerRefFromApplication(app *shipperv1.Application) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: "shipper.booking.com/v1",
		Kind:       "Application",
		Name:       app.GetName(),
		UID:        app.GetUID(),
	}
}
