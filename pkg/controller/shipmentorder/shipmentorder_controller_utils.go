package shipmentorder

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperchart "github.com/bookingcom/shipper/pkg/chart"
)

func (c *Controller) transitionShipmentOrderPhase(so *shipperv1.ShipmentOrder, nextPhase shipperv1.ShipmentOrderPhase) error {
	so.Status.Phase = nextPhase

	// TODO change to UpdateStatus when kubernetes#38113 is merged.
	_, err := c.shipperclientset.ShipperV1().ShipmentOrders(so.Namespace).Update(so)
	if err != nil {
		return err
	}

	c.recorder.Eventf(
		so,
		corev1.EventTypeNormal,
		reasonTransition,
		"Transitioned to %q",
		string(nextPhase),
	)

	return nil
}

func (c *Controller) shipmentOrderHasRelease(so *shipperv1.ShipmentOrder) bool {
	_, err := c.getReleaseForShipmentOrder(so)
	return err == nil
}

func (c *Controller) getReleaseForShipmentOrder(so *shipperv1.ShipmentOrder) (*shipperv1.Release, error) {
	selector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			shipperv1.ReleaseLabel: releaseNameForShipmentOrder(so),
		},
	}

	rlist, err := c.shipperclientset.ShipperV1().Releases(so.Namespace).List(metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, err
	}

	if n := len(rlist.Items); n != 1 {
		names := make([]string, n)
		for i := 0; i < n; i++ {
			names[i] = rlist.Items[i].ObjectMeta.Name
		}
		return nil, fmt.Errorf("expected exactly one Release but found %v", names)
	}

	return &rlist.Items[0], nil
}

func (c *Controller) createReleaseForShipmentOrder(so *shipperv1.ShipmentOrder) error {
	chart, err := downloadChartForShipmentOrder(so)
	if err != nil {
		return err
	}

	b64 := base64.StdEncoding.EncodeToString(chart.Bytes())

	replicas, err := extractReplicasFromChart(chart, so)
	if err != nil {
		return err
	}
	glog.V(6).Infof("Extracted %+v replicas from ShipmentOrder %q", replicas, so.GetName())

	releaseName := releaseNameForShipmentOrder(so)

	release := &shipperv1.Release{
		ReleaseMeta: shipperv1.ReleaseMeta{
			ObjectMeta: metav1.ObjectMeta{
				Name:      releaseName,
				Namespace: so.Namespace,
				Labels: map[string]string{
					shipperv1.ReleaseLabel: releaseName,
				},
			},
			Environment: shipperv1.ReleaseEnvironment{
				Chart: shipperv1.EmbeddedChart{
					Name:    so.Spec.Chart.Name,
					Version: so.Spec.Chart.Version,
					Tarball: b64,
				},
				ShipmentOrder: *so.Spec.DeepCopy(),
				Replicas:      replicas,
			},
		},
		Spec:   shipperv1.ReleaseSpec{},
		Status: shipperv1.WaitingForSchedulingPhase,
	}

	if _, err := c.shipperclientset.ShipperV1().Releases(so.Namespace).Create(release); err != nil {
		return err
	}

	c.recorder.Eventf(
		so,
		corev1.EventTypeNormal,
		reasonShipping,
		"Created Release %q",
		releaseName,
	)

	return nil
}

func extractReplicasFromChart(chart io.Reader, so *shipperv1.ShipmentOrder) (*int32, error) {
	rendered, err := shipperchart.Render(chart, so.ObjectMeta.Name, so.ObjectMeta.Namespace, so.Spec.Values)
	if err != nil {
		return nil, err
	}

	deployments := shipperchart.GetDeployments(rendered)
	if len(deployments) != 1 {
		return nil, fmt.Errorf("expected exactly one Deployment but got %d", len(deployments))
	}

	return deployments[0].Spec.Replicas, nil
}

func downloadChartForShipmentOrder(so *shipperv1.ShipmentOrder) (*bytes.Buffer, error) {
	buf, err := shipperchart.Download(so.Spec.Chart)
	if err != nil {
		return nil, err
	}

	return buf, nil
}

func releaseNameForShipmentOrder(so *shipperv1.ShipmentOrder) string {
	return "release-" + so.ObjectMeta.Name
}
