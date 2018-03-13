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

	mk := metaKey(so)

	rlist, err := c.relLister.Releases(so.GetNamespace()).List(selector)
	if err != nil {
		return nil, fmt.Errorf("list Releases for ShipmentOrder %q: %s", mk, err)
	}

	n := len(rlist)
	glog.V(6).Infof(`Found %d Releases for ShipmentOrder %q using selector %q`, n, mk, selector)
	if n == 0 {
		return nil, fmt.Errorf("list Releases for ShipmentOrder %q: too few", mk)
	} else if n > 1 {
		names := make([]string, n)
		for i := 0; i < n; i++ {
			names[i] = rlist[i].GetName()
		}
		glog.Warningf("expected exactly one Release for ShipmentOrder %q but found %v", mk, names)

		return nil, fmt.Errorf("list Releases for ShipmentOrder %q: too many", mk)
	}

	return rlist[0], nil
}

// findLatestRelease, given a namespace and a selector, finds the latest
// installed Release for an application. Selector needs to cover all Releases of
// a single application.
// Returns an error if more than one Release is found. Returned Release can be
// nil even if there's no error (i.e. no installed Releases).
func (c *Controller) findLatestRelease(ns string, selector labels.Selector) (*shipperv1.Release, error) {
	rlist, err := c.relLister.Releases(ns).List(selector)
	if err != nil {
		return nil, fmt.Errorf("find latest Release for %q in %q: %s", selector, ns, err)
	}

	var (
		already bool
		latest  *shipperv1.Release
	)

	for _, r := range rlist {
		if r.Status.Successor != nil || r.Status.Phase != shipperv1.ReleasePhaseInstalled {
			continue
		}

		if already {
			glog.Warningf("Found at least two installed Releases without a successor: %q and %q", metaKey(latest), metaKey(r))
			return nil, fmt.Errorf("find latest Release for %q in %q: too many", ns, selector)
		}

		already = true
		latest = r
	}

	return latest, nil
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
	releaseNs := so.GetNamespace()

	labels := make(map[string]string)
	for k, v := range so.GetLabels() {
		labels[k] = v
	}

	relSelector, err := metav1.LabelSelectorAsMap(so.Spec.ReleaseSelector)
	if err != nil {
		return fmt.Errorf("Release selector for ShipmentOrder %q: %s", metaKey(so), err)
	} else if relSelector == nil {
		// TODO this should be replaced with an admission hook
		return fmt.Errorf("Release selector for ShipmentOrder %q: not specified", metaKey(so))
	}
	for k, v := range relSelector {
		labels[k] = v
	}

	labels[shipperv1.ReleaseLabel] = releaseNameForShipmentOrder(so)

	selector, _ := metav1.LabelSelectorAsSelector(so.Spec.ReleaseSelector)
	glog.V(6).Infof("Using Release selector %q", selector)

	var (
		new  *shipperv1.Release
		pred *shipperv1.Release
	)

	new = &shipperv1.Release{
		ReleaseMeta: shipperv1.ReleaseMeta{
			ObjectMeta: metav1.ObjectMeta{
				Name:      releaseName,
				Namespace: releaseNs,
				Labels:    labels,
			},
			Environment: shipperv1.ReleaseEnvironment{
				Chart: shipperv1.EmbeddedChart{
					Name:    so.Spec.Chart.Name,
					Version: so.Spec.Chart.Version,
					Tarball: b64,
				},
				ShipmentOrder: *so.Spec.DeepCopy(), // XXX use SerializedReference?
				Replicas:      replicas,
			},
		},
		Spec: shipperv1.ReleaseSpec{},
		Status: shipperv1.ReleaseStatus{
			Phase: shipperv1.ReleasePhaseWaitingForScheduling,
		},
	}

	pred, err = c.findLatestRelease(releaseNs, selector)
	if err != nil {
		return err
	}

	ri := c.shipperClientset.ShipperV1().Releases(releaseNs)

	// It's important that we do Update first. If the update fails, at least there
	// won't be two Releases without a successor. Recovering from this would
	// require reasoning about order of things e.g. based on timestamps. In a
	// distributed system this can be complicated.

	if pred != nil {
		glog.V(4).Infof("Release %q is the predecessor of %q", releaseName, pred.GetName())

		new.Status.Predecessor = &corev1.ObjectReference{
			APIVersion: pred.APIVersion,
			Kind:       pred.Kind,
			Name:       pred.GetName(),
			Namespace:  releaseNs,
		}

		pred.Status.Successor = &corev1.ObjectReference{
			APIVersion: new.APIVersion,
			Kind:       new.Kind,
			Name:       releaseName,
			Namespace:  releaseNs,
		}

		// TODO change to UpdateStatus when kubernetes#38113 is merged.
		// TODO Patch
		if _, err = ri.Update(pred); err != nil {
			// If Update failed, we bail out with pred untouched and new is not created.
			// No recovery needed.
			return fmt.Errorf("set successor for Release %q: %s", metaKey(pred), err)
		}
	} else {
		// Must be a first deployment of a new app.
		glog.V(4).Infof("No predecessor for Release %q", metaKey(new))
	}

	new, err = ri.Create(new)
	if err != nil {
		// If Update went through but Create failed, we have pred pointing to a
		// Release that does not exist. We can recover from this by fixing the
		// dangling successor pointer.
		// Requeue pred directly so that we don't need to wait a full re-sync period
		// before progress can be made.
		c.relWorkqueue.AddRateLimited(metaKey(pred))
		return fmt.Errorf("create Release for ShipmentOrder %q: %s", metaKey(so), err)
	}

	c.recorder.Eventf(
		new,
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
