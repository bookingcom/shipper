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

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"github.com/bookingcom/shipper/pkg/conditions"
	"github.com/bookingcom/shipper/pkg/controller"
	apputil "github.com/bookingcom/shipper/pkg/util/application"
)

func (c *Controller) createReleaseForApplication(app *shipperv1.Application, generation int) error {
	// Label releases with their hash; select by that label and increment if needed
	// appname-hash-of-template-iteration.
	releaseName, iteration, err := c.releaseNameForApplication(app)
	if err != nil {
		return err
	}
	releaseNs := app.GetNamespace()
	glog.V(4).Infof("Generated Release name for Application %q: %q", controller.MetaKey(app), releaseName)

	labels := make(map[string]string)
	for k, v := range app.GetLabels() {
		labels[k] = v
	}

	labels[shipperv1.ReleaseLabel] = releaseName
	labels[shipperv1.AppLabel] = app.GetName()
	labels[shipperv1.ReleaseEnvironmentHashLabel] = hashReleaseEnvironment(app.Spec.Template)

	annotations := map[string]string{
		shipperv1.ReleaseTemplateIterationAnnotation: strconv.Itoa(iteration),
		shipperv1.ReleaseGenerationAnnotation:        strconv.Itoa(generation),
	}

	glog.V(4).Infof("Release %q labels: %v", controller.MetaKey(app), labels)
	glog.V(4).Infof("Release %q annotations: %v", controller.MetaKey(app), annotations)

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
		Spec:   shipperv1.ReleaseSpec{},
		Status: shipperv1.ReleaseStatus{},
	}

	_, err = c.shipperClientset.ShipperV1().Releases(releaseNs).Create(new)
	if err != nil {
		return fmt.Errorf("create Release for Application %q: %s", controller.MetaKey(app), err)
	}
	return nil
}

func (c *Controller) getLatestReleaseForApp(app *shipperv1.Application) (*shipperv1.Release, error) {
	sortedReleases, err := c.getSortedAppReleases(app)
	if err != nil {
		validHistoryCond := apputil.NewApplicationCondition(
			shipperv1.ApplicationConditionTypeValidHistory, corev1.ConditionFalse,
			conditions.FetchReleaseFailed,
			fmt.Sprintf("could not fetch the latest release: %q", err),
		)
		apputil.SetApplicationCondition(&app.Status, *validHistoryCond)

		return nil, err
	}

	if len(sortedReleases) == 0 {
		return nil, nil
	}

	return sortedReleases[len(sortedReleases)-1], nil
}

func (c *Controller) getAppHistory(app *shipperv1.Application) ([]string, error) {
	releases, err := c.getSortedAppReleases(app)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(releases))
	for _, rel := range releases {
		names = append(names, rel.GetName())
	}
	return names, nil
}

func (c *Controller) getSortedAppReleases(app *shipperv1.Application) ([]*shipperv1.Release, error) {
	selector := labels.Set{
		shipperv1.AppLabel: app.GetName(),
	}.AsSelector()

	releases, err := c.relLister.Releases(app.GetNamespace()).List(selector)
	if err != nil {
		return nil, err
	}
	sorted, err := controller.SortReleasesByGeneration(releases)
	if err != nil {
		return nil, err
	}

	return sorted, nil
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
			return "", 0, fmt.Errorf("generate name for Release %q: no iteration annotation",
				controller.MetaKey(rel))
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

func (c *Controller) computeState(app *shipperv1.Application) (shipperv1.ApplicationState, error) {
	state := shipperv1.ApplicationState{}

	latestRelease, err := c.getLatestReleaseForApp(app)
	if err != nil {
		return state, err
	}

	// If there's no history, it means we are about to rollout app but we haven't
	// started yet.
	if latestRelease == nil {
		return state, nil
	}

	state.RolloutStep, _ = isRollingOut(latestRelease)
	return state, nil
}

func isRollingOut(rel *shipperv1.Release) (*int32, bool) {
	lastStep := int32(len(rel.Environment.Strategy.Steps) - 1)

	achieved := rel.Status.AchievedStep
	if achieved == nil {
		return nil, true
	}

	achievedStep := achieved.Step

	targetStep := rel.Spec.TargetStep

	return &achievedStep, achievedStep != targetStep ||
		achievedStep != lastStep
}

func getAppHighestObservedGeneration(app *shipperv1.Application) (int, error) {
	rawObserved, ok := app.Annotations[shipperv1.AppHighestObservedGenerationAnnotation]
	if !ok {
		return 0, nil
	}

	generation, err := strconv.Atoi(rawObserved)
	if err != nil {
		return 0, err
	}

	return generation, nil
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
