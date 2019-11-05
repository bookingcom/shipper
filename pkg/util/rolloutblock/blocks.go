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

// BlocksRollout is used by application and release controllers to check if to block a rollout while updating relevant events
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
		err := shippererrors.NewInvalidRolloutBlockOverrideError(obsoleteBlocks.String())
		return true, events, err
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

func GetAllBlocks(rolloutBlockLister shipperlisters.RolloutBlockLister, obj metav1.Object) (ObjectNameList, ObjectNameList, error) {
	annotations := obj.GetAnnotations()
	overrides := NewObjectNameList(annotations[shipper.RolloutBlocksOverrideAnnotation])
	nsBlocks, err := rolloutBlockLister.RolloutBlocks(obj.GetNamespace()).List(labels.Everything())
	if err != nil {
		return overrides, nil, err
	}
	globalBlocks, err := rolloutBlockLister.RolloutBlocks(shipper.GlobalRolloutBlockNamespace).List(labels.Everything())
	if err != nil {
		return overrides, nil, err
	}
	existingBlocks := NewObjectNameListFromRolloutBlocksList(append(nsBlocks, globalBlocks...))
	return overrides, existingBlocks, nil
}

func ValidateBlocks(existing, overrides ObjectNameList) error {
	effectiveBlocks := existing.Diff(overrides)
	if len(effectiveBlocks) > 0 {
		return shippererrors.NewRolloutBlockError(effectiveBlocks.String())
	}
	return nil
}

func ValidateAnnotations(existing, overrides ObjectNameList) error {
	nonExistingBlocks := overrides.Diff(existing)
	if len(nonExistingBlocks) > 0 {
		return shippererrors.NewInvalidRolloutBlockOverrideError(nonExistingBlocks.String())
	}
	return nil
}
