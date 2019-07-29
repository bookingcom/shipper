package rolloutblock

import (
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

const (
	testAppName          = "test-app"
	testReleaseName      = "test-release"
	testRolloutBlockName = "test-rollout-block"
)

func (f *fixture) newController() (*Controller, shipperinformers.SharedInformerFactory) {
	f.client = shipperfake.NewSimpleClientset(f.objects...)
	const noResyncPeriod time.Duration = 0
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(f.client, noResyncPeriod)

	controller := NewController(f.client, shipperInformerFactory, record.NewFakeRecorder(42))
	return controller, shipperInformerFactory
}

func (f *fixture) run() {
	c, i := f.newController()

	stopCh := make(chan struct{})
	defer close(stopCh)

	i.Start(stopCh)
	i.WaitForCacheSync(stopCh)

	wait.PollUntil(
		10*time.Millisecond,
		func() (bool, error) { return c.rolloutblockWorkqueue.Len() >= 1, nil },
		stopCh,
	)

	c.processNextRolloutBlockWorkItem()

	actual := shippertesting.FilterActions(f.client.Actions())
	shippertesting.CheckActions(f.actions, actual, f.t)
}

// Tests for Namespace RolloutBlocks
func TestNewRolloutBlockWithoutOverrides(t *testing.T) {
	f := newFixture(t)

	app := newApplication(testAppName)
	rel := newRelease(testReleaseName, app)

	rolloutblock := newRolloutBlock(testRolloutBlockName, shippertesting.TestNamespace)
	f.objects = append(f.objects, rolloutblock, app, rel)

	expectedRolloutBlock := rolloutblock.DeepCopy()
	expectedRolloutBlock.Status.Overrides.Application = ""
	expectedRolloutBlock.Status.Overrides.Release = ""

	f.expectRolloutBlockUpdate(expectedRolloutBlock)
	f.run()
}

func TestNewRolloutBlockWithOverrides(t *testing.T) {
	f := newFixture(t)

	app := newApplication(testAppName)
	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testRolloutBlockName)
	rel := newRelease(testReleaseName, app)
	f.objects = append(f.objects, app, rel)

	rolloutBlock := newRolloutBlock(testRolloutBlockName, shippertesting.TestNamespace)
	f.objects = append(f.objects, rolloutBlock)

	expectedRolloutBlock := rolloutBlock.DeepCopy()
	expectedRolloutBlock.Status.Overrides.Application = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testAppName)
	expectedRolloutBlock.Status.Overrides.Release = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testReleaseName)

	f.expectRolloutBlockUpdate(expectedRolloutBlock)
	f.run()
}

func TestAddApplicationToRolloutBlockStatus(t *testing.T) {
	f := newFixture(t)

	rolloutblock := newRolloutBlock(testRolloutBlockName, shippertesting.TestNamespace)
	f.objects = append(f.objects, rolloutblock)

	app := newApplication(testAppName)
	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testRolloutBlockName)

	f.objects = append(f.objects, app)

	expectedRolloutBlock := rolloutblock.DeepCopy()
	expectedRolloutBlock.Status.Overrides.Application = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testAppName)
	expectedRolloutBlock.Status.Overrides.Release = ""

	f.expectRolloutBlockUpdate(expectedRolloutBlock)
	f.run()
}

func TestAddApplicationAndReleaseToRolloutBlockStatus(t *testing.T) {
	f := newFixture(t)

	rolloutblock := newRolloutBlock(testRolloutBlockName, shippertesting.TestNamespace)
	f.objects = append(f.objects, rolloutblock)

	app := newApplication(testAppName)
	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testRolloutBlockName)
	rel := newRelease(testReleaseName, app)

	f.objects = append(f.objects, app, rel)

	expectedRolloutBlock := rolloutblock.DeepCopy()
	expectedRolloutBlock.Status.Overrides.Application = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testAppName)
	expectedRolloutBlock.Status.Overrides.Release = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testReleaseName)

	f.expectRolloutBlockUpdate(expectedRolloutBlock)
	f.run()
}

func TestAddReleaseToRolloutBlockStatus(t *testing.T) {
	f := newFixture(t)

	rolloutblock := newRolloutBlock(testRolloutBlockName, shippertesting.TestNamespace)
	f.objects = append(f.objects, rolloutblock)

	app := newApplication(testAppName)
	rel := newRelease(testReleaseName, app)
	rel.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testRolloutBlockName)

	f.objects = append(f.objects, app, rel)

	expectedRolloutBlock := rolloutblock.DeepCopy()
	expectedRolloutBlock.Status.Overrides.Application = ""
	expectedRolloutBlock.Status.Overrides.Release = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testReleaseName)

	f.expectRolloutBlockUpdate(expectedRolloutBlock)
	f.run()
}

func TestRemoveAppFromRolloutBlockStatus(t *testing.T) {
	f := newFixture(t)

	rolloutblock := newRolloutBlock(testRolloutBlockName, shippertesting.TestNamespace)
	f.objects = append(f.objects, rolloutblock)

	app := newApplication(testAppName)
	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testRolloutBlockName)

	f.objects = append(f.objects, app)

	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = ""

	expectedRolloutBlock := rolloutblock.DeepCopy()
	expectedRolloutBlock.Status.Overrides.Application = ""
	expectedRolloutBlock.Status.Overrides.Release = ""

	f.expectRolloutBlockUpdate(expectedRolloutBlock)
	f.run()
}

func TestRemoveReleaseFromRolloutBlockStatus(t *testing.T) {
	f := newFixture(t)

	rolloutblock := newRolloutBlock(testRolloutBlockName, shippertesting.TestNamespace)
	f.objects = append(f.objects, rolloutblock)

	app := newApplication(testAppName)
	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testRolloutBlockName)
	rel := newRelease(testReleaseName, app)

	f.objects = append(f.objects, app, rel)

	rel.Annotations[shipper.RolloutBlocksOverrideAnnotation] = ""

	expectedRolloutBlock := rolloutblock.DeepCopy()
	expectedRolloutBlock.Status.Overrides.Application = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testAppName)
	expectedRolloutBlock.Status.Overrides.Release = ""

	f.expectRolloutBlockUpdate(expectedRolloutBlock)
	f.run()
}

// Tests for Global RolloutBlocks
func TestNewGlobalRolloutBlockWithoutOverrides(t *testing.T) {
	f := newFixture(t)

	app := newApplication(testAppName)
	rel := newRelease(testReleaseName, app)

	rolloutblock := newRolloutBlock(testRolloutBlockName, shipper.GlobalRolloutBlockNamespace)
	f.objects = append(f.objects, rolloutblock, app, rel)

	expectedRolloutBlock := rolloutblock.DeepCopy()
	expectedRolloutBlock.Status.Overrides.Application = ""
	expectedRolloutBlock.Status.Overrides.Release = ""

	f.expectRolloutBlockUpdate(expectedRolloutBlock)
	f.run()
}

func TestNewGlobalRolloutBlockWithOverrides(t *testing.T) {
	f := newFixture(t)

	app := newApplication(testAppName)
	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", shipper.GlobalRolloutBlockNamespace, testRolloutBlockName)
	rel := newRelease(testReleaseName, app)
	f.objects = append(f.objects, app, rel)

	rolloutBlock := newRolloutBlock(testRolloutBlockName, shipper.GlobalRolloutBlockNamespace)
	f.objects = append(f.objects, rolloutBlock)

	expectedRolloutBlock := rolloutBlock.DeepCopy()
	expectedRolloutBlock.Status.Overrides.Application = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testAppName)
	expectedRolloutBlock.Status.Overrides.Release = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testReleaseName)

	f.expectRolloutBlockUpdate(expectedRolloutBlock)
	f.run()
}

func TestAddApplicationToGlobalRolloutBlockStatus(t *testing.T) {
	f := newFixture(t)

	rolloutblock := newRolloutBlock(testRolloutBlockName, shipper.GlobalRolloutBlockNamespace)
	f.objects = append(f.objects, rolloutblock)

	app := newApplication(testAppName)
	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", shipper.GlobalRolloutBlockNamespace, testRolloutBlockName)

	f.objects = append(f.objects, app)

	expectedRolloutBlock := rolloutblock.DeepCopy()
	expectedRolloutBlock.Status.Overrides.Application = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testAppName)
	expectedRolloutBlock.Status.Overrides.Release = ""

	f.expectRolloutBlockUpdate(expectedRolloutBlock)
	f.run()
}

func TestAddApplicationAndReleaseToGlobalRolloutBlockStatus(t *testing.T) {
	f := newFixture(t)

	rolloutblock := newRolloutBlock(testRolloutBlockName, shipper.GlobalRolloutBlockNamespace)
	f.objects = append(f.objects, rolloutblock)

	app := newApplication(testAppName)
	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", shipper.GlobalRolloutBlockNamespace, testRolloutBlockName)
	rel := newRelease(testReleaseName, app)

	f.objects = append(f.objects, app, rel)

	expectedRolloutBlock := rolloutblock.DeepCopy()
	expectedRolloutBlock.Status.Overrides.Application = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testAppName)
	expectedRolloutBlock.Status.Overrides.Release = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testReleaseName)

	f.expectRolloutBlockUpdate(expectedRolloutBlock)
	f.run()
}

func TestAddReleaseToGlobalRolloutBlockStatus(t *testing.T) {
	f := newFixture(t)

	rolloutblock := newRolloutBlock(testRolloutBlockName, shipper.GlobalRolloutBlockNamespace)
	f.objects = append(f.objects, rolloutblock)

	app := newApplication(testAppName)
	rel := newRelease(testReleaseName, app)
	rel.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", shipper.GlobalRolloutBlockNamespace, testRolloutBlockName)

	f.objects = append(f.objects, app, rel)

	expectedRolloutBlock := rolloutblock.DeepCopy()
	expectedRolloutBlock.Status.Overrides.Application = ""
	expectedRolloutBlock.Status.Overrides.Release = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testReleaseName)

	f.expectRolloutBlockUpdate(expectedRolloutBlock)
	f.run()
}

func TestRemoveAppFromGlobalRolloutBlockStatus(t *testing.T) {
	f := newFixture(t)

	rolloutblock := newRolloutBlock(testRolloutBlockName, shipper.GlobalRolloutBlockNamespace)
	f.objects = append(f.objects, rolloutblock)

	app := newApplication(testAppName)
	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", shipper.GlobalRolloutBlockNamespace, testRolloutBlockName)

	f.objects = append(f.objects, app)

	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = ""

	expectedRolloutBlock := rolloutblock.DeepCopy()
	expectedRolloutBlock.Status.Overrides.Application = ""
	expectedRolloutBlock.Status.Overrides.Release = ""

	f.expectRolloutBlockUpdate(expectedRolloutBlock)
	f.run()
}

func TestRemoveReleaseFromGlobalRolloutBlockStatus(t *testing.T) {
	f := newFixture(t)

	rolloutblock := newRolloutBlock(testRolloutBlockName, shipper.GlobalRolloutBlockNamespace)
	f.objects = append(f.objects, rolloutblock)

	app := newApplication(testAppName)
	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", shipper.GlobalRolloutBlockNamespace, testRolloutBlockName)
	rel := newRelease(testReleaseName, app)

	f.objects = append(f.objects, app, rel)

	rel.Annotations[shipper.RolloutBlocksOverrideAnnotation] = ""

	expectedRolloutBlock := rolloutblock.DeepCopy()
	expectedRolloutBlock.Status.Overrides.Application = fmt.Sprintf("%s/%s", shippertesting.TestNamespace, testAppName)
	expectedRolloutBlock.Status.Overrides.Release = ""

	f.expectRolloutBlockUpdate(expectedRolloutBlock)
	f.run()
}

func (f *fixture) expectRolloutBlockUpdate(rb *shipper.RolloutBlock) {
	gvr := shipper.SchemeGroupVersion.WithResource("rolloutblocks")
	action := kubetesting.NewUpdateAction(gvr, rb.GetNamespace(), rb)

	f.actions = append(f.actions, action)
}

type fixture struct {
	t       *testing.T
	client  *shipperfake.Clientset
	actions []kubetesting.Action
	objects []runtime.Object
}

func newFixture(t *testing.T) *fixture {
	return &fixture{t: t}
}

func newRolloutBlock(name string, namespace string) *shipper.RolloutBlock {
	return &shipper.RolloutBlock{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: shipper.RolloutBlockSpec{
			Message: "Simple test rollout block",
			Author: shipper.RolloutBlockAuthor{
				Type: "user",
				Name: "testUser",
			},
		},
	}
}

func newApplication(name string) *shipper.Application {
	return &shipper.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   shippertesting.TestNamespace,
			Annotations: map[string]string{},
		},
	}
}

func newRelease(releaseName string, app *shipper.Application) *shipper.Release {
	rolloutblocksOverrides := app.Annotations[shipper.RolloutBlocksOverrideAnnotation]
	return &shipper.Release{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "Release",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      releaseName,
			Namespace: app.GetNamespace(),
			Annotations: map[string]string{
				shipper.RolloutBlocksOverrideAnnotation: rolloutblocksOverrides,
			},
			Labels: map[string]string{
				shipper.ReleaseLabel: releaseName,
				shipper.AppLabel:     app.GetName(),
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Kind:       "Application",
					Name:       app.GetName(),
					UID:        app.GetUID(),
				},
			},
		},
		Status: shipper.ReleaseStatus{
			Conditions: []shipper.ReleaseCondition{},
		},
		Spec: shipper.ReleaseSpec{
			Environment: *(app.Spec.Template.DeepCopy()),
		},
	}
}
