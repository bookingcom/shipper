package application

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strconv"

	"github.com/golang/glog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/controller"
	"github.com/bookingcom/shipper/pkg/errors"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

func (c *Controller) createReleaseForApplication(app *shipper.Application, releaseName string, iteration, generation int) (*shipper.Release, error) {
	// Label releases with their hash; select by that label and increment if needed
	// appname-hash-of-template-iteration.

	glog.V(4).Infof("Generated Release name for Application %q: %q", controller.MetaKey(app), releaseName)

	newRelease := &shipper.Release{
		ObjectMeta: metav1.ObjectMeta{
			Name:      releaseName,
			Namespace: app.Namespace,
			Labels: map[string]string{
				shipper.ReleaseLabel:                releaseName,
				shipper.AppLabel:                    app.Name,
				shipper.ReleaseEnvironmentHashLabel: hashReleaseEnvironment(app.Spec.Template),
			},
			Annotations: map[string]string{
				shipper.ReleaseTemplateIterationAnnotation: strconv.Itoa(iteration),
				shipper.ReleaseGenerationAnnotation:        strconv.Itoa(generation),
			},
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromApplication(app),
			},
		},
		Spec: shipper.ReleaseSpec{
			Environment: *(app.Spec.Template.DeepCopy()),
		},
		Status: shipper.ReleaseStatus{},
	}

	for k, v := range app.GetLabels() {
		newRelease.Labels[k] = v
	}

	// application may contain semver range, need to convert it into a specific version
	cv, err := c.versionResolver(&newRelease.Spec.Environment.Chart)
	if err != nil {
		return nil, err
	}
	newRelease.Spec.Environment.Chart.Version = cv.Version

	glog.V(4).Infof("Release %q labels: %v", controller.MetaKey(newRelease), newRelease.Labels)
	glog.V(4).Infof("Release %q annotations: %v", controller.MetaKey(newRelease), newRelease.Annotations)

	rel, err := c.shipperClientset.ShipperV1alpha1().Releases(app.Namespace).Create(newRelease)
	if err != nil {
		return nil, shippererrors.NewKubeclientCreateError(newRelease, err).
			WithShipperKind("Release")
	}

	return rel, nil
}

func (c *Controller) releaseNameForApplication(app *shipper.Application) (string, int, error) {
	hash := hashReleaseEnvironment(app.Spec.Template)
	// TODO(asurikov): move the hash to annotations.
	selector := labels.Set{
		shipper.AppLabel:                    app.GetName(),
		shipper.ReleaseEnvironmentHashLabel: hash,
	}.AsSelector()

	releases, err := c.relLister.Releases(app.GetNamespace()).List(selector)
	if err != nil {
		return "", 0, shippererrors.NewKubeclientListError(
			shipper.SchemeGroupVersion.WithKind("Release"),
			app.GetNamespace(), selector, err)
	}

	if len(releases) == 0 {
		glog.V(3).Infof("No Releases with template %q for Application %q", hash, controller.MetaKey(app))
		return fmt.Sprintf("%s-%s-%d", app.GetName(), hash, 0), 0, nil
	}

	highestObserved := 0
	for _, rel := range releases {
		iterationStr, ok := rel.GetAnnotations()[shipper.ReleaseTemplateIterationAnnotation]
		if !ok {
			return "", 0, errors.NewMissingGenerationAnnotationError(controller.MetaKey(rel))
		}

		iteration, err := strconv.Atoi(iterationStr)
		if err != nil {
			return "", 0, shippererrors.NewUnrecoverableError(err)
		}

		if iteration > highestObserved {
			highestObserved = iteration
		}
	}

	newIteration := highestObserved + 1
	return fmt.Sprintf("%s-%s-%d", app.GetName(), hash, newIteration), newIteration, nil
}

func identicalEnvironments(envs ...shipper.ReleaseEnvironment) bool {
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

func hashReleaseEnvironment(env shipper.ReleaseEnvironment) string {
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

func createOwnerRefFromApplication(app *shipper.Application) metav1.OwnerReference {
	// App's TypeMeta can be empty so can't use it to set APIVersion and Kind. See
	// https://github.com/kubernetes/client-go/issues/60#issuecomment-281533822 and
	// https://github.com/kubernetes/client-go/issues/60#issuecomment-281747911 for
	// context.
	return metav1.OwnerReference{
		APIVersion: shipper.SchemeGroupVersion.String(),
		Kind:       "Application",
		Name:       app.GetName(),
		UID:        app.GetUID(),
	}
}
