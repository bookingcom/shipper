package application

import (
	//	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	metatypes "k8s.io/apimachinery/pkg/types"
	runtimeutil "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	//	"k8s.io/helm/pkg/repo/repotest"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"github.com/bookingcom/shipper/pkg/chart"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

var testApp *shipperv1.Application = newApplication("test-app")

func TestDanglingSuccessorPointer(t *testing.T) {
	f := newFixture(t)

	rel := newRelease("dangling-pointer", testApp)
	rel.Status.Successor = &corev1.ObjectReference{
		Kind:       rel.Kind,
		APIVersion: rel.APIVersion,
		Name:       "does-not-exist",
		Namespace:  rel.GetNamespace(),
	}
	f.objects = append(f.objects, testApp, rel)

	fixedRel := rel.DeepCopy()
	fixedRel.Status.Successor = nil
	f.expectReleaseStatusUpdate(fixedRel)

	f.runForRelease()
}

func TestValidSuccessorPointer(t *testing.T) {
	f := newFixture(t)

	// alpha -> bravo -> charlie aborted
	//                `-> delta
	alpha := newRelease("alpha", testApp)
	bravo := newRelease("bravo", testApp)
	charlie := newRelease("charlie", testApp)
	delta := newRelease("delta", testApp)

	alpha.Status.Successor = &corev1.ObjectReference{
		Kind:       bravo.Kind,
		APIVersion: bravo.APIVersion,
		Name:       bravo.GetName(),
		Namespace:  bravo.GetNamespace(),
	}
	alpha.Status.Phase = shipperv1.ReleasePhaseSuperseded

	bravo.Status.Predecessor = &corev1.ObjectReference{
		Kind:       alpha.Kind,
		APIVersion: alpha.APIVersion,
		Name:       alpha.GetName(),
		Namespace:  alpha.GetNamespace(),
	}
	bravo.Status.Successor = &corev1.ObjectReference{
		Kind:       delta.Kind,
		APIVersion: delta.APIVersion,
		Name:       delta.GetName(),
		Namespace:  delta.GetNamespace(),
	}
	bravo.Status.Phase = shipperv1.ReleasePhaseSuperseded

	charlie.Status.Predecessor = &corev1.ObjectReference{
		Kind:       bravo.Kind,
		APIVersion: bravo.APIVersion,
		Name:       bravo.GetName(),
		Namespace:  bravo.GetNamespace(),
	}
	charlie.Status.Phase = shipperv1.ReleasePhaseAborted

	delta.Status.Predecessor = &corev1.ObjectReference{
		Kind:       bravo.Kind,
		APIVersion: bravo.APIVersion,
		Name:       bravo.GetName(),
		Namespace:  bravo.GetNamespace(),
	}
	delta.Status.Phase = shipperv1.ReleasePhaseInstalled

	f.objects = append(f.objects, alpha, bravo, charlie, delta)

	// expect no actions

	f.runForRelease()
}

func TestAbortedReleaseSuccessor(t *testing.T) {
	f := newFixture(t)

	// alpha -> bravo
	alpha := newRelease("alpha", testApp)
	bravo := newRelease("bravo", testApp)

	alpha.Status.Successor = &corev1.ObjectReference{
		Kind:       bravo.Kind,
		APIVersion: bravo.APIVersion,
		Name:       bravo.GetName(),
		Namespace:  bravo.GetNamespace(),
	}
	alpha.Status.Phase = shipperv1.ReleasePhaseInstalled

	bravo.Status.Predecessor = &corev1.ObjectReference{
		Kind:       alpha.Kind,
		APIVersion: alpha.APIVersion,
		Name:       alpha.GetName(),
		Namespace:  alpha.GetNamespace(),
	}
	bravo.Status.Phase = shipperv1.ReleasePhaseAborted

	f.objects = append(f.objects, testApp, alpha, bravo)

	alphaFixed := alpha.DeepCopy()
	alphaFixed.Status.Successor = nil
	f.expectReleaseStatusUpdate(alphaFixed)

	f.runForRelease()
}

func TestNoSuccessorRelease(t *testing.T) {
	f := newFixture(t)

	alpha := newRelease("alpha", testApp)
	f.objects = append(f.objects, testApp, alpha)

	// expect no actions

	f.runForRelease()
}

func newRelease(name string, app *shipperv1.Application) *shipperv1.Release {
	return &shipperv1.Release{
		ReleaseMeta: shipperv1.ReleaseMeta{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: shippertesting.TestNamespace,
				Labels: map[string]string{
					"app":                  "shipmentorder-controller-test",
					"some-label-key":       "some-label-value",
					shipperv1.ReleaseLabel: name,
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "shipper.booking.com/v1",
						Kind:       "Application",
						Name:       app.GetName(),
						UID:        app.GetUID(),
					},
				},
			},
			Environment: shipperv1.ReleaseEnvironment{
				Chart:            shipperv1.Chart{},
				Strategy:         shipperv1.ReleaseStrategy{Name: "foobar"},
				ClusterSelectors: []shipperv1.ClusterSelector{},
				Values:           &shipperv1.ChartValues{},
				Replicas:         int32(21),
			},
		},
	}
}

func newApplication(name string) *shipperv1.Application {
	return &shipperv1.Application{
		ObjectMeta: metav1.ObjectMeta{
			UID:       metatypes.UID(name),
			Name:      name,
			Namespace: shippertesting.TestNamespace,
			Labels: map[string]string{
				shipperv1.AppLabel: name,
			},
		},
		Spec: shipperv1.ApplicationSpec{
			Template: shipperv1.ReleaseEnvironment{
				Chart:            shipperv1.Chart{},
				Strategy:         shipperv1.ReleaseStrategy{Name: "foobar"},
				ClusterSelectors: []shipperv1.ClusterSelector{},
				Values:           &shipperv1.ChartValues{},
				Replicas:         int32(21),
			},
		},
	}
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

func (f *fixture) newController() (*Controller, shipperinformers.SharedInformerFactory) {
	f.client = shipperfake.NewSimpleClientset(f.objects...)

	const noResyncPeriod time.Duration = 0
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(f.client, noResyncPeriod)

	c := NewController(f.client, shipperInformerFactory, record.NewFakeRecorder(42), chart.FetchRemote())

	return c, shipperInformerFactory
}

func (f *fixture) run() {
	c, i := f.newController()

	stopCh := make(chan struct{})
	defer close(stopCh)

	i.Start(stopCh)
	i.WaitForCacheSync(stopCh)

	wait.PollUntil(
		10*time.Millisecond,
		func() (bool, error) { return c.appWorkqueue.Len() >= 1, nil },
		stopCh,
	)

	processNextWorkItem(c.appWorkqueue, c.syncApplication)

	actual := shippertesting.FilterActions(f.client.Actions())
	shippertesting.CheckActions(f.actions, actual, f.t)
}

func (f *fixture) runForRelease() {
	c, i := f.newController()

	stopCh := make(chan struct{})
	defer close(stopCh)

	i.Start(stopCh)
	i.WaitForCacheSync(stopCh)

	wait.PollUntil(
		10*time.Millisecond,
		func() (bool, error) { return c.relWorkqueue.Len() >= 1, nil },
		stopCh,
	)

	runtimeutil.ErrorHandlers = []func(error){
		func(err error) {
			f.t.Errorf("got an error: %q", err)
		},
	}

	for c.relWorkqueue.Len() > 0 {
		processNextWorkItem(c.relWorkqueue, c.syncRelease)
	}

	actual := shippertesting.FilterActions(f.client.Actions())
	shippertesting.CheckActions(f.actions, actual, f.t)
}

func (f *fixture) expectReleaseCreate(rel *shipperv1.Release) {
	gvr := shipperv1.SchemeGroupVersion.WithResource("releases")
	action := kubetesting.NewCreateAction(gvr, rel.GetNamespace(), rel)

	f.actions = append(f.actions, action)
}

func (f *fixture) expectReleaseStatusUpdate(rel *shipperv1.Release) {
	gvr := shipperv1.SchemeGroupVersion.WithResource("releases")
	action := kubetesting.NewUpdateAction(gvr, rel.GetNamespace(), rel)
	// TODO uncomment when kubernetes#38113 is merged
	//action.Subresource = "status"

	f.actions = append(f.actions, action)
}
