package shipmentorder

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperchart "github.com/bookingcom/shipper/pkg/chart"
)

func (c *Controller) transitionShipmentOrderPhase(so *shipperv1.ShipmentOrder, nextPhase shipperv1.ShipmentOrderPhase) error {
	prevPhase := so.Status.Phase
	so.Status.Phase = nextPhase

	// TODO change to UpdateStatus when kubernetes#38113 is merged.
	_, err := c.shipperClientset.ShipperV1().ShipmentOrders(so.Namespace).Update(so)
	if err != nil {
		return fmt.Errorf(`transition ShipmentOrder %q to %q: %s`, metaKey(so), nextPhase, err)
	}

	c.recorder.Eventf(
		so,
		corev1.EventTypeNormal,
		reasonTransition,
		"%q -> %q",
		prevPhase,
		nextPhase,
	)

	return nil
}

func (c *Controller) shipmentOrderHasRelease(so *shipperv1.ShipmentOrder) bool {
	_, err := c.getReleaseForShipmentOrder(so)
	return err == nil
}

func (c *Controller) getReleaseForShipmentOrder(so *shipperv1.ShipmentOrder) (*shipperv1.Release, error) {
	selector := labels.Set{
		shipperv1.ReleaseLabel: releaseNameForShipmentOrder(so),
	}.AsSelector()

	rlist, err := c.shipperClientset.ShipperV1().Releases(so.Namespace).List(metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("list Releases for ShipmentOrder %q: %s", metaKey(so), err)
	}

	n := len(rlist.Items)
	glog.V(6).Infof(`Found %d Releases for ShipmentOrder %q using selector %q`, n, metaKey(so), selector)
	if n != 1 {
		names := make([]string, n)
		for i := 0; i < n; i++ {
			names[i] = rlist.Items[i].GetName()
		}
		return nil, fmt.Errorf("list Releases for ShipmentOrder %q: expected exactly one Release but found %v", metaKey(so), names)
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
	glog.V(6).Infof(`Extracted %+v replicas from ShipmentOrder %q`, replicas, metaKey(so))

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
		Spec: shipperv1.ReleaseSpec{},
		Status: shipperv1.ReleaseStatus{
			Phase: shipperv1.ReleasePhaseWaitingForScheduling,
		},
	}

	if _, err := c.shipperClientset.ShipperV1().Releases(so.Namespace).Create(release); err != nil {
		return fmt.Errorf("create Release for ShipmentOrder %q: %s", metaKey(so), err)
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

func extractReplicasFromChart(chart io.Reader, so *shipperv1.ShipmentOrder) (int32, error) {
	rendered, err := shipperchart.Render(chart, so.ObjectMeta.Name, so.ObjectMeta.Namespace, so.Spec.Values)
	if err != nil {
		return 0, fmt.Errorf("extract replicas for ShipmentOrder %q: %s", metaKey(so), err)
	}

	deployments := shipperchart.GetDeployments(rendered)
	if n := len(deployments); n != 1 {
		return 0, fmt.Errorf("extract replicas for ShipmentOrder %q: expected exactly one Deployment but got %d", metaKey(so), n)
	}

	replicas := deployments[0].Spec.Replicas
	// deployments default to 1 replica when replicas is nil or unspecified
	// see k8s.io/api/apps/v1/types.go DeploymentSpec
	if replicas == nil {
		return 1, nil
	}

	return *replicas, nil
}

func downloadChartForShipmentOrder(so *shipperv1.ShipmentOrder) (*bytes.Buffer, error) {
	buf, err := shipperchart.Download(so.Spec.Chart)
	if err != nil {
		return nil, fmt.Errorf("download chart for ShipmentOrder %q: %s", metaKey(so), err)
	}

	return buf, nil
}

func releaseNameForShipmentOrder(so *shipperv1.ShipmentOrder) string {
	return "release-" + so.GetName()
}

func metaKey(obj runtime.Object) string {
	key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	return key
}
