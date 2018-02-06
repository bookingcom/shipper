package shipmentorder

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

func TestSyncOnePending(t *testing.T) {
	t.Fatal("not implemented")
}

func TestSyncOneShipping(t *testing.T) {
	t.Fatal("not implemented")
}

func TestSyncOneShipped(t *testing.T) {
	t.Fatal("not implemented")
}

func TestSyncOnePartial(t *testing.T) {
	t.Fatal("not implemented")
}

func getShipmentOrder() *shipperv1.ShipmentOrder {
	return &shipperv1.ShipmentOrder{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ship-it",
			Namespace: "default",
		},
		Spec: shipperv1.ShipmentOrderSpec{
			Strategy: shipperv1.ReleaseStrategyVanguard,
			ClusterSelectors: []shipperv1.ClusterSelector{
				shipperv1.ClusterSelector{
					Regions:      []string{"eu"},
					Capabilities: []string{"gpu"},
				},
			},
			Chart: shipperv1.Chart{
				Name:    "test-application",
				Version: "1.0",
				RepoURL: "https://chart-museum.local",
			},
			Values: &shipperv1.ChartValues{
				"foo": "bar",
			},
		},
		Status: shipperv1.ShipmentOrderStatus{
			Phase: shipperv1.ShipmentOrderPhasePending,
		},
	}
}
