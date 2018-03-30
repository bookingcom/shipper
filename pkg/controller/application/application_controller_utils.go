package application

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strconv"

	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	helmchart "k8s.io/helm/pkg/proto/hapi/chart"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperchart "github.com/bookingcom/shipper/pkg/chart"
)

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

	new := &shipperv1.Release{
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

	new.Environment.Replicas = int32(replicas)

	// create the entry in release history in state 'WaitingForObject'
	err = c.appendReleaseToAppHistory(releaseName, app)
	if err != nil {
		return err
	}

	var existingReleaseObject *shipperv1.Release
	existingReleaseObject, err = c.relLister.Releases(app.GetNamespace()).Get(releaseName)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	// this likely means we failed to mark the release created in a previous run
	if existingReleaseObject != nil {
		return c.markReleaseCreated(releaseName, app)
	}

	_, err = c.shipperClientset.ShipperV1().Releases(releaseNs).Create(new)
	if err != nil {
		return fmt.Errorf("create Release for Application %q: %s", metaKey(app), err)
	}

	return c.markReleaseCreated(releaseName, app)
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

func (c *Controller) appendReleaseToAppHistory(releaseName string, app *shipperv1.Application) error {
	var last *shipperv1.ReleaseRecord
	if len(app.Status.History) > 0 {
		last = app.Status.History[len(app.Status.History)-1]
	}

	// already here
	if last != nil && last.Name == releaseName {
		return nil
	}

	app.Status.History = append(app.Status.History, &shipperv1.ReleaseRecord{
		Name:   releaseName,
		Status: shipperv1.ReleaseRecordWaitingForObject,
	})

	_, err := c.shipperClientset.ShipperV1().Applications(app.Namespace).Update(app)
	return err
}

func (c *Controller) cleanHistory(app *shipperv1.Application) error {
	cleaned := make([]*shipperv1.ReleaseRecord, 0, len(app.Status.History))
	changed := false
	for _, record := range app.Status.History {
		_, err := c.relLister.Releases(app.GetNamespace()).Get(record.Name)
		if err != nil {
			if errors.IsNotFound(err) {
				changed = true
				continue
			}
			return err
		}

		cleaned = append(cleaned, record)
	}

	if !changed {
		return nil
	}

	app.Status.History = cleaned
	_, err := c.shipperClientset.ShipperV1().Applications(app.Namespace).Update(app)
	return err
}

func (c *Controller) markReleaseCreated(releaseName string, app *shipperv1.Application) error {
	changed := false
	for _, record := range app.Status.History {
		if record.Name == releaseName {
			record.Status = shipperv1.ReleaseRecordObjectCreated
			changed = true
		}
	}

	if !changed {
		return fmt.Errorf(
			"could not mark %q as created: it does not have a record in app %q history",
			releaseName, metaKey(app),
		)
	}

	_, err := c.shipperClientset.ShipperV1().Applications(app.Namespace).Update(app)
	return err
}

func (c *Controller) getEndOfHistory(app *shipperv1.Application) *shipperv1.ReleaseRecord {
	if len(app.Status.History) == 0 {
		return nil
	}

	return app.Status.History[len(app.Status.History)-1]
}

func (c *Controller) rollbackAppTemplate(app *shipperv1.Application) error {
	history := app.Status.History
	ns := app.GetNamespace()

	// march backwards through history to find a release object which exists.
	// once we find it, reset app template to that and trim history after that
	// entry

	// NOTE: if we didn't find _any_ release then the template will remain in
	// place and we'll end up re-creating a release from current app template.
	// I think that's the best we can do in this case.
	var goodRelease *shipperv1.Release
	i := len(history) - 1
	for ; i >= 0; i-- {
		record := history[i]
		rel, err := c.relLister.Releases(ns).Get(record.Name)
		if err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return err
		}
		goodRelease = rel
		break
	}

	// truncate history to the release we found (or to empty if we didn't find any)
	app.Status.History = history[:i+1]
	if goodRelease != nil {
		app.Spec.Template = *(goodRelease.Environment.DeepCopy())
	}

	_, err := c.shipperClientset.ShipperV1().Applications(app.Namespace).Update(app)
	return err
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
	copy := env.DeepCopy()
	// TODO(btyler) move these to a non-environment field
	copy.Clusters = nil
	copy.Replicas = 0
	b, err := json.Marshal(copy)
	if err != nil {
		// TODO(btyler) ???
		panic(err)
	}

	hash := fnv.New32a()
	_, _ = hash.Write(b)
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
