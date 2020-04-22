package conditions

import (
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

// StrategyConditionsShouldDiscardTimestamps can be used to skip timestamps in
// condition transitions in tests.
var StrategyConditionsShouldDiscardTimestamps = false

// StrategyConditionsMap is used to manage a list of ReleaseStrategyConditions.
type StrategyConditionsMap map[shipper.StrategyConditionType]shipper.ReleaseStrategyCondition

type StrategyConditionsUpdate struct {
	Reason             string
	Message            string
	Step               int32
	LastTransitionTime time.Time
}

// NewStrategyConditions returns a new StrategyConditionsMap object with the
// given list of ReleaseStrategyConditions.
func NewStrategyConditions(conditions ...shipper.ReleaseStrategyCondition) StrategyConditionsMap {
	sc := make(StrategyConditionsMap)

	sc.Set(conditions...)

	return sc
}

// SetTrue transitions an existing condition from its current status to True.
func (sc StrategyConditionsMap) SetTrue(conditionType shipper.StrategyConditionType, update StrategyConditionsUpdate) {
	sc.update(conditionType, corev1.ConditionTrue, update)
}

// SetFalse transitions an existing condition from its current status to False.
func (sc StrategyConditionsMap) SetFalse(conditionType shipper.StrategyConditionType, update StrategyConditionsUpdate) {
	sc.update(conditionType, corev1.ConditionFalse, update)
}

// SetUnknown transitions an existing condition from its current status to Unknown.
func (sc StrategyConditionsMap) SetUnknown(conditionType shipper.StrategyConditionType, update StrategyConditionsUpdate) {
	sc.update(conditionType, corev1.ConditionUnknown, update)
}

// Merge merges another StrategyConditionsMap object into the receiver.
// Conditions from "other" can override existing conditions in the receiver.
func (sc StrategyConditionsMap) Merge(other StrategyConditionsMap) {
	for _, e := range other {
		sc[e.Type] = e
	}
}

// Set adds conditions to the receiver. Added conditions can override existing
// conditions in the receiver.
func (sc StrategyConditionsMap) Set(conditions ...shipper.ReleaseStrategyCondition) {
	for _, e := range conditions {
		sc[e.Type] = e
	}
}

// AsReleaseStrategyConditions returns an ordered list of
// v1.ReleaseStrategyCondition values. This list is sorted by
// ReleaseStrategyCondition type.
func (sc StrategyConditionsMap) AsReleaseStrategyConditions() []shipper.ReleaseStrategyCondition {
	var conditionTypesAsString []string
	var conditions []shipper.ReleaseStrategyCondition

	for k := range sc {
		conditionTypesAsString = append(conditionTypesAsString, string(k))
	}

	sort.Strings(conditionTypesAsString)

	for _, v := range conditionTypesAsString {
		condition := sc[shipper.StrategyConditionType(v)]
		if StrategyConditionsShouldDiscardTimestamps {
			condition.LastTransitionTime = metav1.Time{}
		}
		conditions = append(conditions, condition)
	}

	return conditions
}

// IsUnknown returns true if all the given conditions have their status Unknown in the receiver.
func (sc StrategyConditionsMap) IsUnknown(conditionTypes ...shipper.StrategyConditionType) bool {
	return sc.isState(corev1.ConditionUnknown, conditionTypes...)
}

// IsFalse returns true if all the given conditions have their status False in the receiver.
func (sc StrategyConditionsMap) IsFalse(conditionTypes ...shipper.StrategyConditionType) bool {
	return sc.isState(corev1.ConditionFalse, conditionTypes...)
}

// IsTrue returns true if all the given conditions have their status True in the receiver.
func (sc StrategyConditionsMap) IsTrue(conditionTypes ...shipper.StrategyConditionType) bool {
	return sc.isState(corev1.ConditionTrue, conditionTypes...)
}

// AllTrue returns true if all the existing conditions in the receiver have
// their status True.
func (sc StrategyConditionsMap) AllTrue() bool {
	return sc.isState(corev1.ConditionTrue, sc.allConditionTypes()...)
}

func (sc StrategyConditionsMap) IsNotTrue(step int32, conditionTypes ...shipper.StrategyConditionType) bool {
	for _, conditionType := range conditionTypes {
		if c, ok := sc.GetCondition(conditionType); ok && c.Step == step && c.Status == corev1.ConditionTrue {
			return false
		}
	}
	return true
}

// allConditionTypes returns an unordered list of all conditions in the receiver.
func (sc StrategyConditionsMap) allConditionTypes() []shipper.StrategyConditionType {
	conditionTypes := make([]shipper.StrategyConditionType, 0, len(sc))
	for _, v := range sc {
		conditionTypes = append(conditionTypes, v.Type)
	}
	return conditionTypes
}

// GetStatus returns the status of condition from the receiver.
func (sc StrategyConditionsMap) GetStatus(conditionType shipper.StrategyConditionType) (corev1.ConditionStatus, bool) {
	if aCondition, ok := sc[conditionType]; !ok {
		return corev1.ConditionUnknown, false
	} else {
		return aCondition.Status, true
	}
}

func (sc StrategyConditionsMap) update(
	conditionType shipper.StrategyConditionType,
	newStatus corev1.ConditionStatus,
	update StrategyConditionsUpdate,
) {

	existingCondition, ok := sc[conditionType]
	if !ok {
		lastTransitionTime := metav1.NewTime(update.LastTransitionTime)

		newCondition := shipper.ReleaseStrategyCondition{
			Type:               conditionType,
			Status:             newStatus,
			Reason:             update.Reason,
			Message:            update.Message,
			LastTransitionTime: lastTransitionTime,
			Step:               update.Step,
		}

		sc[conditionType] = newCondition
	} else {
		lastTransitionTime := existingCondition.LastTransitionTime

		if newStatus != existingCondition.Status || lastTransitionTime.IsZero() {
			lastTransitionTime = metav1.NewTime(update.LastTransitionTime)
		}

		newCondition := shipper.ReleaseStrategyCondition{
			Type:               existingCondition.Type,
			Status:             newStatus,
			Reason:             update.Reason,
			Message:            update.Message,
			LastTransitionTime: lastTransitionTime,
			Step:               update.Step,
		}

		sc[conditionType] = newCondition
	}
}

func (sc StrategyConditionsMap) isState(
	status corev1.ConditionStatus,
	conditionTypes ...shipper.StrategyConditionType,
) bool {
	for _, conditionType := range conditionTypes {
		if c, ok := sc.GetCondition(conditionType); !ok {
			return false
		} else if c.Status != status {
			return false
		}
	}
	return true
}

func (sc StrategyConditionsMap) GetCondition(conditionType shipper.StrategyConditionType) (shipper.ReleaseStrategyCondition, bool) {
	c, ok := sc[conditionType]
	return c, ok
}
