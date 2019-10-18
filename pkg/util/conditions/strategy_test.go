package conditions

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

// - Serialization: AsReleaseStrategyConditions, AsReleaseStrategyState
// - Transitions

func TestNonExistingToTrue(t *testing.T) {

	ct := shipper.StrategyConditionContenderAchievedInstallation

	now := time.Now()

	sc := NewStrategyConditions()

	sc.SetTrue(ct, StrategyConditionsUpdate{
		Step:               0,
		LastTransitionTime: now,
	})

	testTransitionAndUpdateTimes(t, sc, ct, now, now)

}

func TestNonExistingToFalse(t *testing.T) {

	ct := shipper.StrategyConditionContenderAchievedInstallation

	now := time.Now()

	sc := NewStrategyConditions()

	sc.SetTrue(ct, StrategyConditionsUpdate{
		Step:               0,
		LastTransitionTime: now,
	})

	testTransitionAndUpdateTimes(t, sc, ct, now, now)
}

func TestNonExistingToUnknown(t *testing.T) {

	ct := shipper.StrategyConditionContenderAchievedInstallation

	now := time.Now()

	sc := NewStrategyConditions()

	sc.SetTrue(ct, StrategyConditionsUpdate{
		Step:               0,
		LastTransitionTime: now,
	})

	testTransitionAndUpdateTimes(t, sc, ct, now, now)
}

func TestTrueToTrue(t *testing.T) {
	ct := shipper.StrategyConditionContenderAchievedInstallation

	createTime := time.Now()
	updateTime := createTime.Add(time.Second * 2)

	sc := NewStrategyConditions(
		shipper.ReleaseStrategyCondition{
			Type:               ct,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.NewTime(createTime),
		},
	)

	sc.SetTrue(ct, StrategyConditionsUpdate{
		Step:               0,
		LastTransitionTime: createTime,
	})

	testTransitionAndUpdateTimes(t, sc, ct, createTime, updateTime)
}

func TestFalseToFalse(t *testing.T) {
	ct := shipper.StrategyConditionContenderAchievedInstallation

	createTime := time.Now()
	updateTime := createTime.Add(time.Second * 2)

	sc := NewStrategyConditions(
		shipper.ReleaseStrategyCondition{
			Type:               ct,
			Status:             corev1.ConditionFalse,
			LastTransitionTime: metav1.NewTime(createTime),
		},
	)

	sc.SetFalse(ct, StrategyConditionsUpdate{
		Step:               0,
		LastTransitionTime: createTime,
		Reason:             ClustersNotReady,
	})

	testTransitionAndUpdateTimes(t, sc, ct, createTime, updateTime)
}

func TestUnknownToUnknown(t *testing.T) {
	ct := shipper.StrategyConditionContenderAchievedInstallation

	createTime := time.Now()
	updateTime := createTime.Add(time.Second * 2)

	sc := NewStrategyConditions(
		shipper.ReleaseStrategyCondition{
			Type:               ct,
			Status:             corev1.ConditionUnknown,
			LastTransitionTime: metav1.NewTime(createTime),
		},
	)

	sc.SetUnknown(ct, StrategyConditionsUpdate{
		Step:               0,
		LastTransitionTime: createTime,
	})

	testTransitionAndUpdateTimes(t, sc, ct, createTime, updateTime)
}

func TestUnknownToTrue(t *testing.T) {
	ct := shipper.StrategyConditionContenderAchievedInstallation

	now := time.Now()

	transitionTime := now.Add(time.Second * 2)
	updateTime := now.Add(time.Second * 2)

	step0 := int32(0)

	sc := NewStrategyConditions(
		shipper.ReleaseStrategyCondition{
			Type:               ct,
			Status:             corev1.ConditionUnknown,
			LastTransitionTime: metav1.NewTime(now),
			Step:               step0,
		},
	)

	if !sc.IsUnknown(0, ct) {
		t.Errorf("condition should be Unknown")
	}

	sc.SetTrue(ct, StrategyConditionsUpdate{
		Step:               0,
		LastTransitionTime: transitionTime,
	})

	testTransitionAndUpdateTimes(t, sc, ct, transitionTime, updateTime)
}

func TestUnknownToFalse(t *testing.T) {

	ct := shipper.StrategyConditionContenderAchievedInstallation

	now := time.Now()

	transitionTime := now.Add(time.Second * 2)
	updateTime := now.Add(time.Second * 2)

	sc := NewStrategyConditions(
		shipper.ReleaseStrategyCondition{
			Type:   ct,
			Status: corev1.ConditionUnknown,
		},
	)

	if !sc.IsUnknown(0, ct) {
		t.Errorf("condition should be Unknown")
	}

	sc.SetFalse(
		ct,
		StrategyConditionsUpdate{
			Reason:             ClustersNotReady,
			Step:               0,
			LastTransitionTime: transitionTime,
		})

	if !sc.IsFalse(0, ct) {
		t.Errorf("condition should be False")
	}

	testTransitionAndUpdateTimes(t, sc, ct, transitionTime, updateTime)
}

func TestContenderStateWaitingForCapacity(t *testing.T) {
	step0 := int32(0)
	step1 := int32(1)
	sc := NewStrategyConditions(
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedInstallation,
			Status: corev1.ConditionTrue,
			Step:   step1,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedCapacity,
			Status: corev1.ConditionFalse,
			Reason: ClustersNotReady,
			Step:   step1,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedTraffic,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionIncumbentAchievedCapacity,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionIncumbentAchievedCapacity,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
	)

	expected := shipper.ReleaseStrategyState{
		WaitingForCapacity:     shipper.StrategyStateTrue,
		WaitingForInstallation: shipper.StrategyStateFalse,
		WaitingForTraffic:      shipper.StrategyStateFalse,
		WaitingForCommand:      shipper.StrategyStateFalse,
	}

	releaseStrategyState := sc.AsReleaseStrategyState(step1, true, false)
	if !reflect.DeepEqual(releaseStrategyState, expected) {
		t.Fatalf(
			"Strategy states are different\nDiff:\n %s",
			cmp.Diff(releaseStrategyState, expected))
	}
}

func TestContenderStateWaitingForTraffic(t *testing.T) {
	step0 := int32(0)
	step1 := int32(1)
	sc := NewStrategyConditions(
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedInstallation,
			Status: corev1.ConditionTrue,
			Step:   step1,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedCapacity,
			Status: corev1.ConditionTrue,
			Reason: ClustersNotReady,
			Step:   step1,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedTraffic,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionIncumbentAchievedCapacity,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionIncumbentAchievedCapacity,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
	)

	expected := shipper.ReleaseStrategyState{
		WaitingForCapacity:     shipper.StrategyStateFalse,
		WaitingForInstallation: shipper.StrategyStateFalse,
		WaitingForTraffic:      shipper.StrategyStateTrue,
		WaitingForCommand:      shipper.StrategyStateFalse,
	}

	releaseStrategyState := sc.AsReleaseStrategyState(step1, true, false)
	if !reflect.DeepEqual(releaseStrategyState, expected) {
		t.Fatalf(
			"Strategy states are different\nDiff:\n %s",
			cmp.Diff(releaseStrategyState, expected))
	}
}

func TestIncumbentStateWaitingForTraffic(t *testing.T) {
	step0 := int32(0)
	step1 := int32(1)
	sc := NewStrategyConditions(
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedInstallation,
			Status: corev1.ConditionTrue,
			Step:   step1,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedCapacity,
			Status: corev1.ConditionTrue,
			Step:   step1,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedTraffic,
			Status: corev1.ConditionTrue,
			Step:   step1,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionIncumbentAchievedCapacity,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionIncumbentAchievedTraffic,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
	)

	expected := shipper.ReleaseStrategyState{
		WaitingForCapacity:     shipper.StrategyStateFalse,
		WaitingForInstallation: shipper.StrategyStateFalse,
		WaitingForTraffic:      shipper.StrategyStateTrue,
		WaitingForCommand:      shipper.StrategyStateFalse,
	}

	releaseStrategyState := sc.AsReleaseStrategyState(step1, true, false)
	if !reflect.DeepEqual(releaseStrategyState, expected) {
		t.Fatalf(
			"Strategy states are different\nDiff:\n %s",
			cmp.Diff(releaseStrategyState, expected))
	}
}

func TestIncumbentStateWaitingForCapacity(t *testing.T) {
	step0 := int32(0)
	step1 := int32(1)
	sc := NewStrategyConditions(
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedInstallation,
			Status: corev1.ConditionTrue,
			Step:   step1,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedCapacity,
			Status: corev1.ConditionTrue,
			Step:   step1,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedTraffic,
			Status: corev1.ConditionTrue,
			Step:   step1,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionIncumbentAchievedCapacity,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionIncumbentAchievedTraffic,
			Status: corev1.ConditionTrue,
			Step:   step1,
		},
	)

	expected := shipper.ReleaseStrategyState{
		WaitingForCapacity:     shipper.StrategyStateTrue,
		WaitingForInstallation: shipper.StrategyStateFalse,
		WaitingForTraffic:      shipper.StrategyStateFalse,
		WaitingForCommand:      shipper.StrategyStateFalse,
	}

	releaseStrategyState := sc.AsReleaseStrategyState(step1, true, false)
	if !reflect.DeepEqual(releaseStrategyState, expected) {
		t.Fatalf(
			"Strategy states are different\nDiff:\n %s",
			cmp.Diff(releaseStrategyState, expected))
	}
}

func TestStateWaitingForCommand(t *testing.T) {
	step1 := int32(1)
	sc := NewStrategyConditions(
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedInstallation,
			Status: corev1.ConditionTrue,
			Step:   step1,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedCapacity,
			Status: corev1.ConditionTrue,
			Step:   step1,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedTraffic,
			Status: corev1.ConditionTrue,
			Step:   step1,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionIncumbentAchievedCapacity,
			Status: corev1.ConditionTrue,
			Step:   step1,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionIncumbentAchievedTraffic,
			Status: corev1.ConditionTrue,
			Step:   step1,
		},
	)

	expected := shipper.ReleaseStrategyState{
		WaitingForCapacity:     shipper.StrategyStateFalse,
		WaitingForInstallation: shipper.StrategyStateFalse,
		WaitingForTraffic:      shipper.StrategyStateFalse,
		WaitingForCommand:      shipper.StrategyStateTrue,
	}

	releaseStrategyState := sc.AsReleaseStrategyState(step1, true, false)
	if !reflect.DeepEqual(releaseStrategyState, expected) {
		t.Fatalf(
			"Strategy states are different\nDiff:\n %s",
			cmp.Diff(releaseStrategyState, expected))
	}
}

func TestContenderAchievedInstallationCondition(t *testing.T) {
	sc := NewStrategyConditions()

	step0 := int32(0)
	sc.SetTrue(
		shipper.StrategyConditionContenderAchievedInstallation,
		StrategyConditionsUpdate{
			Step: step0,
		},
	)
	expected := []shipper.ReleaseStrategyCondition{
		{
			Type:   shipper.StrategyConditionContenderAchievedInstallation,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
	}

	got := sc.AsReleaseStrategyConditions()
	if !reflect.DeepEqual(expected, got) {
		t.Fatalf(
			"ReleaseStrategyConditions are different\nDiff:\n %s",
			cmp.Diff(expected, got))
	}
}

func TestContenderAchievedTrafficCondition(t *testing.T) {
	step0 := int32(0)

	sc := NewStrategyConditions(
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedInstallation,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedCapacity,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
	)

	sc.SetTrue(
		shipper.StrategyConditionContenderAchievedTraffic,
		StrategyConditionsUpdate{
			Step: step0,
		},
	)
	expected := []shipper.ReleaseStrategyCondition{
		{
			Type:   shipper.StrategyConditionContenderAchievedCapacity,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		{
			Type:   shipper.StrategyConditionContenderAchievedInstallation,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		{
			Type:   shipper.StrategyConditionContenderAchievedTraffic,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
	}

	got := sc.AsReleaseStrategyConditions()
	if !reflect.DeepEqual(expected, got) {
		t.Fatalf(
			"ReleaseStrategyConditions are different\nDiff:\n %s",
			cmp.Diff(expected, got))
	}
}

func TestContenderAchievedCapacityCondition(t *testing.T) {
	step0 := int32(0)

	sc := NewStrategyConditions(
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedInstallation,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
	)

	sc.SetTrue(
		shipper.StrategyConditionContenderAchievedCapacity,
		StrategyConditionsUpdate{
			Step: step0,
		},
	)
	expected := []shipper.ReleaseStrategyCondition{
		{
			Type:   shipper.StrategyConditionContenderAchievedCapacity,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		{
			Type:   shipper.StrategyConditionContenderAchievedInstallation,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
	}

	got := sc.AsReleaseStrategyConditions()
	if !reflect.DeepEqual(expected, got) {
		t.Fatalf(
			"ReleaseStrategyConditions are different\nDiff:\n %s",
			cmp.Diff(expected, got))
	}
}

func TestIncumbentAchievedTrafficCondition(t *testing.T) {
	step0 := int32(0)

	sc := NewStrategyConditions(
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedInstallation,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedCapacity,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedTraffic,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
	)

	sc.SetTrue(
		shipper.StrategyConditionIncumbentAchievedTraffic,
		StrategyConditionsUpdate{
			Step: step0,
		},
	)
	expected := []shipper.ReleaseStrategyCondition{
		{
			Type:   shipper.StrategyConditionContenderAchievedCapacity,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		{
			Type:   shipper.StrategyConditionContenderAchievedInstallation,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		{
			Type:   shipper.StrategyConditionContenderAchievedTraffic,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		{
			Type:   shipper.StrategyConditionIncumbentAchievedTraffic,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
	}

	got := sc.AsReleaseStrategyConditions()
	if !reflect.DeepEqual(expected, got) {
		t.Fatalf(
			"ReleaseStrategyConditions are different\nDiff:\n %s",
			cmp.Diff(expected, got))
	}
}

func TestIncumbentAchievedCapacityCondition(t *testing.T) {
	step0 := int32(0)

	sc := NewStrategyConditions(
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedInstallation,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedCapacity,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionContenderAchievedTraffic,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		shipper.ReleaseStrategyCondition{
			Type:   shipper.StrategyConditionIncumbentAchievedTraffic,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
	)

	sc.SetTrue(
		shipper.StrategyConditionIncumbentAchievedCapacity,
		StrategyConditionsUpdate{
			Step: step0,
		},
	)
	expected := []shipper.ReleaseStrategyCondition{
		{
			Type:   shipper.StrategyConditionContenderAchievedCapacity,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		{
			Type:   shipper.StrategyConditionContenderAchievedInstallation,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		{
			Type:   shipper.StrategyConditionContenderAchievedTraffic,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		{
			Type:   shipper.StrategyConditionIncumbentAchievedCapacity,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
		{
			Type:   shipper.StrategyConditionIncumbentAchievedTraffic,
			Status: corev1.ConditionTrue,
			Step:   step0,
		},
	}

	got := sc.AsReleaseStrategyConditions()
	if !reflect.DeepEqual(expected, got) {
		t.Fatalf(
			"ReleaseStrategyConditions are different\nDiff:\n %s",
			cmp.Diff(expected, got))
	}
}

func TestStrategyConditions_AsList(t *testing.T) {

	contenderAchievedInstallation := shipper.StrategyConditionContenderAchievedInstallation
	contenderAchievedCapacity := shipper.StrategyConditionContenderAchievedCapacity
	contenderAchievedTraffic := shipper.StrategyConditionContenderAchievedTraffic

	c := NewStrategyConditions(
		shipper.ReleaseStrategyCondition{
			Type:   contenderAchievedInstallation,
			Status: corev1.ConditionUnknown,
		},
		shipper.ReleaseStrategyCondition{
			Type:   contenderAchievedCapacity,
			Status: corev1.ConditionUnknown,
		},
		shipper.ReleaseStrategyCondition{
			Type:   contenderAchievedTraffic,
			Status: corev1.ConditionUnknown,
		},
	)

	cList := c.AsReleaseStrategyConditions()
	gotNames := make([]string, len(cList))
	for i, e := range cList {
		gotNames[i] = string(e.Type)
	}

	expectedNames := []string{
		string(contenderAchievedCapacity),
		string(contenderAchievedInstallation),
		string(contenderAchievedTraffic),
	}

	if !reflect.DeepEqual(gotNames, expectedNames) {
		t.Errorf("should be ordered")
	}
}

func testTransitionAndUpdateTimes(
	t *testing.T,
	sc StrategyConditionsMap,
	ct shipper.StrategyConditionType,
	transitionTime time.Time,
	updateTime time.Time,
) {
	c, ok := sc.GetCondition(ct)
	if !ok {
		t.Fatalf("expected condition %q not found", ct)
	}

	if c.LastTransitionTime != metav1.NewTime(transitionTime) {
		t.Errorf("transition times are different")
	}
}
