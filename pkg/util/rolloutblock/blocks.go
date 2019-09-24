package rolloutblock

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

type RolloutBlockEvent struct {
	Type    string
	Reason  string
	Message string
}

func BlocksRollout(rolloutBlockLister shipperlisters.RolloutBlockLister, obj metav1.Object) (bool, []RolloutBlockEvent, error) {
	events := []RolloutBlockEvent{}
	annotations := obj.GetAnnotations()
	overrides := NewObjectNameList(annotations[shipper.RolloutBlocksOverrideAnnotation])

	nsBlocks, err := rolloutBlockLister.RolloutBlocks(obj.GetNamespace()).List(labels.Everything())
	if err != nil {
		return false, events, err
	}

	globalBlocks, err := rolloutBlockLister.RolloutBlocks(shipper.GlobalRolloutBlockNamespace).List(labels.Everything())
	if err != nil {
		return false, events, err
	}

	existingBlocks := NewObjectNameListFromRolloutBlocksList(append(nsBlocks, globalBlocks...))
	obsoleteBlocks := overrides.Diff(existingBlocks)

	if len(obsoleteBlocks) > 0 {
		for o := range obsoleteBlocks {
			overrides.Delete(o)
		}
		annotations[shipper.RolloutBlocksOverrideAnnotation] = overrides.String()
		events = append(events, RolloutBlockEvent{
			corev1.EventTypeWarning,
			"OverriddenRolloutBlockNotFound",
			obsoleteBlocks.String()})
	}

	obj.SetAnnotations(annotations)

	effectiveBlocks := existingBlocks.Diff(overrides)

	if len(effectiveBlocks) == 0 {
		if len(overrides) > 0 {
			events = append(events, RolloutBlockEvent{
				corev1.EventTypeNormal,
				"RolloutBlockOverridden",
				overrides.String()})
		}

		return false, events, nil
	} else {
		events = append(events, RolloutBlockEvent{
			corev1.EventTypeWarning,
			"RolloutBlocked",
			effectiveBlocks.String()})
		return true, events, shippererrors.NewRolloutBlockError(effectiveBlocks.String())
	}
}
