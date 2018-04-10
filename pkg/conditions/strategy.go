package conditions

import (
	"sort"
	"time"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StrategyConditionsShouldDiscardTimestamps can be used to skip timestamps in condition transitions in tests.
var StrategyConditionsShouldDiscardTimestamps = false

// StrategyConditionsMap is used to manage a list of ReleaseStrategyConditions.
type StrategyConditionsMap map[shipperV1.StrategyConditionType]shipperV1.ReleaseStrategyCondition

type StrategyConditionsUpdate struct {
	Reason             string
	Message            string
	Step               int32
	LastTransitionTime time.Time
}

// NewStrategyConditions returns a new StrategyConditionsMap object with the
// given list of ReleaseStrategyConditions.
func NewStrategyConditions(conditions ...shipperV1.ReleaseStrategyCondition) StrategyConditionsMap {
	sc := make(StrategyConditionsMap)

	sc.Set(conditions...)

	return sc
}

// SetTrue transitions an existing condition from its current status to True.
func (sc StrategyConditionsMap) SetTrue(conditionType shipperV1.StrategyConditionType, update StrategyConditionsUpdate) error {
	return sc.update(conditionType, coreV1.ConditionTrue, update)
}

// SetFalse transitions an existing condition from its current status to False.
func (sc StrategyConditionsMap) SetFalse(conditionType shipperV1.StrategyConditionType, update StrategyConditionsUpdate) error {
	return sc.update(conditionType, coreV1.ConditionFalse, update)
}

// SetUnknown transitions an existing condition from its current status to Unknown.
func (sc StrategyConditionsMap) SetUnknown(conditionType shipperV1.StrategyConditionType, update StrategyConditionsUpdate) error {
	return sc.update(conditionType, coreV1.ConditionUnknown, update)
}

// Merge merges another StrategyConditionsMap object into the receiver. Conditions
// from "other" can override existing conditions in the receiver.
func (sc StrategyConditionsMap) Merge(other StrategyConditionsMap) {
	for _, e := range other {
		sc[e.Type] = e
	}
}

// Set adds conditions to the receiver. Added conditions can override existing
// conditions in the receiver.
func (sc StrategyConditionsMap) Set(conditions ...shipperV1.ReleaseStrategyCondition) {
	for _, e := range conditions {
		sc[e.Type] = e
	}
}

// AsReleaseStrategyConditions returns an ordered list of
// v1.ReleaseStrategyCondition values. This list is sorted by
// ReleaseStrategyCondition type.
func (sc StrategyConditionsMap) AsReleaseStrategyConditions() []shipperV1.ReleaseStrategyCondition {
	var conditionTypesAsString []string
	var conditions []shipperV1.ReleaseStrategyCondition

	for k := range sc {
		conditionTypesAsString = append(conditionTypesAsString, string(k))
	}

	sort.Strings(conditionTypesAsString)

	for _, v := range conditionTypesAsString {
		condition := sc[shipperV1.StrategyConditionType(v)]
		if StrategyConditionsShouldDiscardTimestamps {
			condition.LastTransitionTime = metaV1.Time{}
		}
		conditions = append(conditions, condition)
	}

	return conditions
}

// IsUnknown returns true if all the given conditions have their status Unknown in the receiver.
func (sc StrategyConditionsMap) IsUnknown(step int32, conditionTypes ...shipperV1.StrategyConditionType) bool {
	return sc.isState(coreV1.ConditionUnknown, step, conditionTypes...)
}

// IsFalse returns true if all the given conditions have their status False in the receiver.
func (sc StrategyConditionsMap) IsFalse(step int32, conditionTypes ...shipperV1.StrategyConditionType) bool {
	return sc.isState(coreV1.ConditionFalse, step, conditionTypes...)
}

// IsTrue returns true if all the given conditions have their status True in the receiver.
func (sc StrategyConditionsMap) IsTrue(step int32, conditionTypes ...shipperV1.StrategyConditionType) bool {
	return sc.isState(coreV1.ConditionTrue, step, conditionTypes...)
}

// AllTrue returns true if all the existing conditions in the receiver have their status True.
func (sc StrategyConditionsMap) AllTrue(step int32) bool {
	return sc.isState(coreV1.ConditionTrue, step, sc.allConditionTypes()...)
}

func (sc StrategyConditionsMap) IsNotTrue(step int32, conditionTypes ...shipperV1.StrategyConditionType) bool {
	for _, conditionType := range conditionTypes {
		if c, ok := sc.GetCondition(conditionType); ok && c.Step == step && c.Status == coreV1.ConditionTrue {
			return false
		}
	}
	return true
}

// allConditionTypes returns an unordered list of all conditions in the receiver.
func (sc StrategyConditionsMap) allConditionTypes() []shipperV1.StrategyConditionType {
	conditionTypes := make([]shipperV1.StrategyConditionType, 0, len(sc))
	for _, v := range sc {
		conditionTypes = append(conditionTypes, v.Type)
	}
	return conditionTypes
}

// AsReleaseStrategyState returns a ReleaseStrategyState computed from the conditions in the receiver.
func (sc StrategyConditionsMap) AsReleaseStrategyState(
	step int32,
	hasIncumbent bool,
	isLastStep bool,
) shipperV1.ReleaseStrategyState {

	// States we don't know just yet are set to Unknown
	state := shipperV1.ReleaseStrategyState{
		WaitingForCapacity:     shipperV1.StrategyStateUnknown,
		WaitingForCommand:      shipperV1.StrategyStateUnknown,
		WaitingForInstallation: shipperV1.StrategyStateUnknown,
		WaitingForTraffic:      shipperV1.StrategyStateUnknown,
	}

	// WaitingForCommand

	achievedInstallation := sc.IsTrue(step, shipperV1.StrategyConditionContenderAchievedInstallation)
	contenderAchievedCapacity := sc.IsTrue(step, shipperV1.StrategyConditionContenderAchievedCapacity)
	contenderAchievedTraffic := sc.IsTrue(step, shipperV1.StrategyConditionContenderAchievedTraffic)
	incumbentAchievedCapacity := sc.IsTrue(step, shipperV1.StrategyConditionIncumbentAchievedCapacity)
	incumbentAchievedTraffic := sc.IsTrue(step, shipperV1.StrategyConditionIncumbentAchievedTraffic)

	// WaitingForInstallation
	if !achievedInstallation {
		state.WaitingForInstallation = shipperV1.StrategyStateTrue
	} else {
		state.WaitingForInstallation = shipperV1.StrategyStateFalse
	}

	// WaitingForCapacity
	//
	// - ContenderAchievedInstall = True && ContenderAchievedCapacity = False
	// - ContenderAchievedCapacity = True && IncumbentAchievedCapacity != True

	contenderWaitingForCapacity := achievedInstallation &&
		!contenderAchievedCapacity

	incumbentWaitingForCapacity := false
	if hasIncumbent {
		incumbentWaitingForCapacity = achievedInstallation &&
			contenderAchievedCapacity &&
			contenderAchievedTraffic &&
			incumbentAchievedTraffic &&
			!incumbentAchievedCapacity
	}

	waitingForCapacity := contenderWaitingForCapacity || incumbentWaitingForCapacity

	if waitingForCapacity {
		state.WaitingForCapacity = shipperV1.StrategyStateTrue
	} else {
		state.WaitingForCapacity = shipperV1.StrategyStateFalse
	}

	// WaitingForTraffic
	contenderWaitingForTraffic := achievedInstallation &&
		contenderAchievedCapacity &&
		!contenderAchievedTraffic

	incumbentWaitingForTraffic := false
	if hasIncumbent {
		incumbentWaitingForTraffic = achievedInstallation &&
			contenderAchievedCapacity &&
			contenderAchievedTraffic &&
			!incumbentAchievedTraffic &&
			!incumbentAchievedCapacity
	}

	waitingForTraffic := contenderWaitingForTraffic || incumbentWaitingForTraffic

	if waitingForTraffic {
		state.WaitingForTraffic = shipperV1.StrategyStateTrue
	} else {
		state.WaitingForTraffic = shipperV1.StrategyStateFalse
	}

	waitingForCommandFlag := !isLastStep &&
		!waitingForCapacity &&
		!waitingForTraffic &&
		achievedInstallation

	if waitingForCommandFlag {
		state.WaitingForCommand = shipperV1.StrategyStateTrue
	} else {
		state.WaitingForCommand = shipperV1.StrategyStateFalse
	}

	return state
}

// GetStatus returns the status of condition from the receiver.
func (sc StrategyConditionsMap) GetStatus(conditionType shipperV1.StrategyConditionType) (coreV1.ConditionStatus, bool) {
	if aCondition, ok := sc[conditionType]; !ok {
		return coreV1.ConditionUnknown, false
	} else {
		return aCondition.Status, true
	}
}

func (sc StrategyConditionsMap) update(
	conditionType shipperV1.StrategyConditionType,
	newStatus coreV1.ConditionStatus,
	update StrategyConditionsUpdate,
) error {

	existingCondition, ok := sc[conditionType]
	if !ok {
		lastTransitionTime := metaV1.NewTime(update.LastTransitionTime)

		newCondition := shipperV1.ReleaseStrategyCondition{
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
			lastTransitionTime = metaV1.NewTime(update.LastTransitionTime)
		}

		newCondition := shipperV1.ReleaseStrategyCondition{
			Type:               existingCondition.Type,
			Status:             newStatus,
			Reason:             update.Reason,
			Message:            update.Message,
			LastTransitionTime: lastTransitionTime,
			Step:               update.Step,
		}

		sc[conditionType] = newCondition
	}

	return nil
}

func (sc StrategyConditionsMap) isState(
	status coreV1.ConditionStatus,
	step int32,
	conditionTypes ...shipperV1.StrategyConditionType,
) bool {
	for _, conditionType := range conditionTypes {
		if c, ok := sc.GetCondition(conditionType); !ok {
			return false
		} else if c.Step != step {
			return false
		} else if c.Status != status {
			return false
		}
	}
	return true
}

func (sc StrategyConditionsMap) GetCondition(conditionType shipperV1.StrategyConditionType) (shipperV1.ReleaseStrategyCondition, bool) {
	c, ok := sc[conditionType]
	return c, ok
}
