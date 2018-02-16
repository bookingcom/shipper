package shipmentorder

import (
	"os"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"k8s.io/helm/pkg/repo/repotest"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
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

	// `base64 -i pkg/controller/shipmentorder/testdata/simple-0.0.1.tgz` on OS X
	const tar = `H4sIFAAAAAAA/ykAK2FIUjBjSE02THk5NWIzVjBkUzVpWlM5Nk9WVjZNV2xqYW5keVRRbz1IZWxtAOyUz077MAzHd85T+AW2pt26n9TrjyMSByTuXmtYhJNGiVdpb4/SbWUrB5AYoEn9XNLGf2Ir+Toa65my/1sMstij5dn10Vrrf2XZr1rr8arzZTF89/t5sVwVM/iBUj6yi4JhpvV384ybuxHQmycK0bSugi5XDcU6GC/9/+PWeEtOHkJDAerWSWiZKYBQFEDvlUNLFRzekOpOefRCL3L1151NfIWj/oWsZxSKWUOe23269quNg8/0Xxbrkf7L1Xo96f83eDWuqeBuuHR1PhDQ+5h1ubIk2KBgpQAuJA/AuCGOyQDJfbBET3XaDeTZ1BgryAsFEImpljYcAixKvb0/y3CZA+D0LI/uZ2Uk+CJyHAtwqiGRphcaR2Hwnx9bsWjckMJYfKEKkL1xVPWKkMFYt9aia94PnEO2MS4TNAzzZ8ga6jK3Y55m38TExA3wFgAA//9zRQvpAAwAAA==`
	relName := releaseNameForShipmentOrder(so)
	twelve := int32(12)
	rel := &shipperv1.Release{
		ReleaseMeta: shipperv1.ReleaseMeta{
			ObjectMeta: metav1.ObjectMeta{
				Name:      relName,
				Namespace: so.GetNamespace(),
				Labels: map[string]string{
					shipperv1.ReleaseLabel: relName,
				},
			},
			Environment: shipperv1.ReleaseEnvironment{
				Chart: shipperv1.EmbeddedChart{
					Name:    so.Spec.Chart.Name,
					Version: so.Spec.Chart.Version,
					Tarball: tar,
				},
				ShipmentOrder: so.DeepCopy().Spec,
				Replicas:      twelve,
			},
		},
		Spec: shipperv1.ReleaseSpec{},
		Status: shipperv1.ReleaseStatus{
			Phase: shipperv1.ReleasePhaseWaitingForScheduling,
		},
	}
	f.expectReleaseCreate(rel)

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
	rel := &shipperv1.Release{
		ReleaseMeta: shipperv1.ReleaseMeta{
			ObjectMeta: metav1.ObjectMeta{
				Name:      relName,
				Namespace: so.GetNamespace(),
				Labels: map[string]string{
					shipperv1.ReleaseLabel: relName,
				},
			},
			Environment: shipperv1.ReleaseEnvironment{
				Chart: shipperv1.EmbeddedChart{
					Name:    so.Spec.Chart.Name,
					Version: so.Spec.Chart.Version,
				},
				ShipmentOrder: so.DeepCopy().Spec,
			},
		},
		Spec: shipperv1.ReleaseSpec{},
		Status: shipperv1.ReleaseStatus{
			Phase: shipperv1.ReleasePhaseWaitingForScheduling,
		},
	}
	f.objects = append(f.objects, rel)

	shippedSo := so.DeepCopy()
	shippedSo.Status.Phase = shipperv1.ShipmentOrderPhaseShipped
	f.expectShipmentOrderStatusUpdate(shippedSo)

	f.run()
}

func newShipmentOrder(phase shipperv1.ShipmentOrderPhase) *shipperv1.ShipmentOrder {
	return &shipperv1.ShipmentOrder{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ship-it",
			Namespace: "shipper-test",
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
		},
		Status: shipperv1.ShipmentOrderStatus{
			Phase: phase,
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

	c := NewController(f.client, shipperInformerFactory, record.NewFakeRecorder(42))

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
		func() (bool, error) { return c.workqueue.Len() >= 1, nil },
		stopCh,
	)

	c.processNextWorkItem()

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
