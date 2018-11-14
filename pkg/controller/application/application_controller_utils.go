package application

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strconv"

	"github.com/golang/glog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"github.com/bookingcom/shipper/pkg/controller"
	"github.com/bookingcom/shipper/pkg/errors"
)

func (c *Controller) createReleaseForApplication(app *shipperv1.Application, releaseName string, iteration, generation int) (*shipperv1.Release, error) {
	// Label releases with their hash; select by that label and increment if needed
	// appname-hash-of-template-iteration.

	glog.V(4).Infof("Generated Release name for Application %q: %q", controller.MetaKey(app), releaseName)

	newRelease := &shipperv1.Release{
		ObjectMeta: metav1.ObjectMeta{
			Name:      releaseName,
			Namespace: app.Namespace,
			Labels: map[string]string{
				shipperv1.ReleaseLabel:                releaseName,
				shipperv1.AppLabel:                    app.Name,
				shipperv1.ReleaseEnvironmentHashLabel: hashReleaseEnvironment(app.Spec.Template),
			},
			Annotations: map[string]string{
				shipperv1.ReleaseTemplateIterationAnnotation: strconv.Itoa(iteration),
				shipperv1.ReleaseGenerationAnnotation:        strconv.Itoa(generation),
			},
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromApplication(app),
			},
		},
		Spec: shipperv1.ReleaseSpec{
			Environment: *(app.Spec.Template.DeepCopy()),
		},
		Status: shipperv1.ReleaseStatus{},
	}

	for k, v := range app.GetLabels() {
		newRelease.Labels[k] = v
	}

	// propagate annotation from application to release object
	for k, v := range app.GetAnnotations() {
		newRelease.Annotations[k] = v
	}:

	glog.V(4).Infof("Release %q labels: %v", controller.MetaKey(app), newRelease.Labels)
	glog.V(4).Infof("Release %q annotations: %v", controller.MetaKey(app), newRelease.Annotations)

	rel, err := c.shipperClientset.ShipperV1().Releases(app.Namespace).Create(newRelease)
	if err != nil {
		return nil, fmt.Errorf("create Release for Application %q: %s", controller.MetaKey(app), err)
	}
	return rel, nil
}

func (c *Controller) releaseNameForApplication(app *shipperv1.Application) (string, int, error) {
	hash := hashReleaseEnvironment(app.Spec.Template)
	// TODO(asurikov): move the hash to annotations.
	selector := labels.Set{
		shipperv1.AppLabel:                    app.GetName(),
		shipperv1.ReleaseEnvironmentHashLabel: hash,
	}.AsSelector()

	releases, err := c.relLister.Releases(app.GetNamespace()).List(selector)
	if err != nil {
		return "", 0, err
	}

	if len(releases) == 0 {
		glog.V(3).Infof("No Releases with template %q for Application %q", hash, controller.MetaKey(app))
		return fmt.Sprintf("%s-%s-%d", app.GetName(), hash, 0), 0, nil
	}

	highestObserved := 0
	for _, rel := range releases {
		iterationStr, ok := rel.GetAnnotations()[shipperv1.ReleaseTemplateIterationAnnotation]
		if !ok {
			return "", 0, errors.NewMissingGenerationAnnotationError(controller.MetaKey(rel))
		}

		iteration, err := strconv.Atoi(iterationStr)
		if err != nil {
			return "", 0, fmt.Errorf("generate name for Release %q: %s",
				controller.MetaKey(rel), err)
		}

		if iteration > highestObserved {
			highestObserved = iteration
		}
	}

	newIteration := highestObserved + 1
	return fmt.Sprintf("%s-%s-%d", app.GetName(), hash, newIteration), newIteration, nil
}

func identicalEnvironments(envs ...shipperv1.ReleaseEnvironment) bool {
	if len(envs) == 0 {
		return true
	}

	referenceHash := hashReleaseEnvironment(envs[0])
	for _, env := range envs[1:] {
		currentHash := hashReleaseEnvironment(env)
		glog.V(4).Infof("Comparing ReleaseEnvironments: %q vs %q", referenceHash, currentHash)

		if referenceHash != currentHash {
			return false
		}
	}
	return true
}

func hashReleaseEnvironment(env shipperv1.ReleaseEnvironment) string {
	copy := env.DeepCopy()
	b, err := json.Marshal(copy)
	if err != nil {
		// TODO(btyler) ???
		panic(err)
	}

	hash := fnv.New32a()
	hash.Write(b)
	return fmt.Sprintf("%x", hash.Sum32())
}

func createOwnerRefFromApplication(app *shipperv1.Application) metav1.OwnerReference {
	// App's TypeMeta can be empty so can't use it to set APIVersion and Kind. See
	// https://github.com/kubernetes/client-go/issues/60#issuecomment-281533822 and
	// https://github.com/kubernetes/client-go/issues/60#issuecomment-281747911 for
	// context.
	return metav1.OwnerReference{
		APIVersion: "shipper.booking.com/v1",
		Kind:       "Application",
		Name:       app.GetName(),
		UID:        app.GetUID(),
	}
}
