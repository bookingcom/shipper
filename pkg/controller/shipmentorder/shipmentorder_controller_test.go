package shipmentorder

import (
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"k8s.io/helm/pkg/repo/repotest"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"github.com/bookingcom/shipper/pkg/chart"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

func TestPendingToShipping(t *testing.T) {
	f := newFixture(t)

	so := newShipmentOrder(shipperv1.ShipmentOrderPhasePending)
	f.objects = append(f.objects, so)

	shippingSo := so.DeepCopy()
	shippingSo.Status.Phase = shipperv1.ShipmentOrderPhaseShipping
	f.expectShipmentOrderStatusUpdate(shippingSo)

	f.run()
}

func TestShippingToShipped(t *testing.T) {
	srv, hh, err := repotest.NewTempServer("testdata/*.tgz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		os.RemoveAll(hh.String())
		srv.Stop()
	}()

	f := newFixture(t)

	so := newShipmentOrder(shipperv1.ShipmentOrderPhaseShipping)
	so.Spec.Chart.RepoURL = srv.URL()
	f.objects = append(f.objects, so)

	oldRel := newRelease("running-now")
	oldRel.Status.Phase = shipperv1.ReleasePhaseInstalled
	f.objects = append(f.objects, oldRel)

	relName := releaseNameForShipmentOrder(so)

	// Expect "running-now" to have its Predecessor set. Other fields don't matter.
	oldRelWithSucc := oldRel.DeepCopy()
	oldRelWithSucc.Status.Successor = &corev1.ObjectReference{
		Kind:       oldRel.Kind,
		APIVersion: oldRel.APIVersion,
		Name:       relName,
		Namespace:  oldRel.GetNamespace(),
	}
	f.expectReleaseStatusUpdate(oldRelWithSucc)

	// Expect a new Release created with all the fields set correctly.
	newRel := newRelease(relName)
	newRel.Environment.Chart = shipperv1.Chart{
		Name:    so.Spec.Chart.Name,
		Version: so.Spec.Chart.Version,
		RepoURL: so.Spec.Chart.RepoURL,
	}
	newRel.Environment.ShipmentOrder = so.DeepCopy().Spec
	newRel.Environment.Replicas = int32(12) // set in the chart
	newRel.Status.Phase = shipperv1.ReleasePhaseWaitingForScheduling
	newRel.Status.Predecessor = &corev1.ObjectReference{
		Kind:       oldRel.Kind,
		APIVersion: oldRel.APIVersion,
		Name:       oldRel.GetName(),
		Namespace:  oldRel.GetNamespace(),
	}
	f.expectReleaseCreate(newRel)

	// Expect the ShipmentOrder to transition to "Shipped".
	shippedSo := so.DeepCopy()
	shippedSo.Status.Phase = shipperv1.ShipmentOrderPhaseShipped
	f.expectShipmentOrderStatusUpdate(shippedSo)

	f.run()
}

func TestShippingToShippedWithExistingRelease(t *testing.T) {
	f := newFixture(t)

	so := newShipmentOrder(shipperv1.ShipmentOrderPhaseShipping)
	f.objects = append(f.objects, so)

	relName := releaseNameForShipmentOrder(so)

	rel := newRelease(relName)
	f.objects = append(f.objects, rel)

	shippedSo := so.DeepCopy()
	shippedSo.Status.Phase = shipperv1.ShipmentOrderPhaseShipped
	f.expectShipmentOrderStatusUpdate(shippedSo)

	f.run()
}

func TestDanglingSuccessorPointer(t *testing.T) {
	f := newFixture(t)

	rel := newRelease("dangling-pointer")
	rel.Status.Successor = &corev1.ObjectReference{
		Kind:       rel.Kind,
		APIVersion: rel.APIVersion,
		Name:       "does-not-exist",
		Namespace:  rel.GetNamespace(),
	}
	f.objects = append(f.objects, rel)

	fixedRel := rel.DeepCopy()
	fixedRel.Status.Successor = nil
	f.expectReleaseStatusUpdate(fixedRel)

	f.runForRelease()
}

func TestValidSuccessorPointer(t *testing.T) {
	f := newFixture(t)

	// alpha -> bravo -> charlie aborted
	//                `-> delta
	alpha := newRelease("alpha")
	bravo := newRelease("bravo")
	charlie := newRelease("charlie")
	delta := newRelease("delta")

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
	alpha := newRelease("alpha")
	bravo := newRelease("bravo")

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

	f.objects = append(f.objects, alpha, bravo)

	alphaFixed := alpha.DeepCopy()
	alphaFixed.Status.Successor = nil
	f.expectReleaseStatusUpdate(alphaFixed)

	f.runForRelease()
}

func TestNoSuccessorRelease(t *testing.T) {
	f := newFixture(t)

	alpha := newRelease("alpha")
	f.objects = append(f.objects, alpha)

	// expect no actions

	f.runForRelease()
}

func newShipmentOrder(phase shipperv1.ShipmentOrderPhase) *shipperv1.ShipmentOrder {
	return &shipperv1.ShipmentOrder{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ship-it",
			Namespace: shippertesting.TestNamespace,
			Labels: map[string]string{
				"some-label-key": "some-label-value",
			},
		},
		Spec: shipperv1.ShipmentOrderSpec{
			ClusterSelectors: []shipperv1.ClusterSelector{
				shipperv1.ClusterSelector{
					Regions:      []string{"eu"},
					Capabilities: []string{"gpu"},
				},
			},
			Chart: shipperv1.Chart{
				Name:    "simple",
				Version: "0.0.1",
				RepoURL: "http://127.0.0.1:8879/charts",
			},
			Strategy: shipperv1.ReleaseStrategyVanguard,
			ReleaseSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "shipmentorder-controller-test",
				},
			},
		},
		Status: shipperv1.ShipmentOrderStatus{
			Phase: phase,
		},
	}
}

func newRelease(name string) *shipperv1.Release {
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
			},
			Environment: shipperv1.ReleaseEnvironment{
				Chart:         shipperv1.Chart{},
				ShipmentOrder: shipperv1.ShipmentOrderSpec{},
				Replicas:      int32(21),
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
		func() (bool, error) { return c.soWorkqueue.Len() >= 1, nil },
		stopCh,
	)

	processNextWorkItem(c.soWorkqueue, c.syncShipmentOrder)

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

	processNextWorkItem(c.relWorkqueue, c.syncRelease)

	actual := shippertesting.FilterActions(f.client.Actions())
	shippertesting.CheckActions(f.actions, actual, f.t)
}

func (f *fixture) expectShipmentOrderStatusUpdate(so *shipperv1.ShipmentOrder) {
	gvr := shipperv1.SchemeGroupVersion.WithResource("shipmentorders")
	action := kubetesting.NewUpdateAction(gvr, so.GetNamespace(), so)
	// TODO uncomment when kubernetes#38113 is merged
	//action.Subresource = "status"

	f.actions = append(f.actions, action)
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
