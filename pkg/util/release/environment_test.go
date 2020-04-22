package release

import (
	"fmt"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

func TestIsReleaseProgressing(t *testing.T) {
	var tests = []struct {
		title        string
		achievedStep *shipper.AchievedStep
		targetStep   int32
		expected     bool
	}{
		{
			"nil achieved step",
			nil,
			0,
			true,
		},
		{
			"empty achieved step",
			&shipper.AchievedStep{},
			0,
			true,
		},
		{
			"progressing release",
			&shipper.AchievedStep{
				Step: 0,
				Name: "0",
			},
			2,
			true,
		},
		{
			"not progressing release",
			&shipper.AchievedStep{
				Step: 1,
				Name: "1",
			},
			0,
			false,
		},
	}
	for _, test := range tests {
		actual := IsReleaseProgressing(test.achievedStep, test.targetStep)
		if actual != test.expected {
			t.Fatalf("testing %s: expected %t, actual %t", test.title, test.expected, actual)
		}
	}
}

func TestStepsFromAchievedStep(t *testing.T) {
	var tests = []struct {
		title        string
		achievedStep *shipper.AchievedStep
		targetStep   int32
		expected     int
	}{
		{
			"nil achieved step",
			nil,
			0,
			0,
		},
		{
			"empty achieved step",
			&shipper.AchievedStep{},
			0,
			0,
		},
		{
			"achieved step",
			&shipper.AchievedStep{
				Step: 0,
			},
			0,
			0,
		},
		{
			"did not achieved step",
			&shipper.AchievedStep{
				Step: 1,
			},
			2,
			1,
		},
	}

	for _, test := range tests {
		actual := StepsFromAchievedStep(test.achievedStep, test.targetStep)
		if actual != test.expected {
			t.Fatalf("testing %s: expected %d, actual %d", test.title, test.expected, actual)
		}
	}
}

func TestGetTargetStep(t *testing.T) {
	var tests = []struct {
		title                 string
		rel                   *shipper.Release
		succ                  *shipper.Release
		strategy              *shipper.RolloutStrategy
		stepsFromAchievedStep int
		progressing           bool
		achievedTargetStep    bool
		expectedTargetStep    int32
		expectedError         error
	}{
		{
			title: "current progressing release one step target step",
			rel: &shipper.Release{
				Spec: shipper.ReleaseSpec{
					TargetStep: 0,
				},
			},
			succ: nil,
			strategy: &shipper.RolloutStrategy{
				Steps: []shipper.RolloutStrategyStep{
					{},
				},
			},
			stepsFromAchievedStep: 1,
			progressing:           true,
			achievedTargetStep:    true,
			expectedTargetStep:    0,
			expectedError:         nil,
		},
		{
			title: "current non progressing release one step target step",
			rel: &shipper.Release{
				Spec: shipper.ReleaseSpec{
					TargetStep: 0,
				},
			},
			succ: nil,
			strategy: &shipper.RolloutStrategy{
				Steps: []shipper.RolloutStrategyStep{
					{},
					{},
				},
			},
			stepsFromAchievedStep: 1,
			progressing:           false,
			achievedTargetStep:    true,
			expectedTargetStep:    0,
			expectedError:         nil,
		},
		{
			title: "current progressing release two steps target step",
			rel: &shipper.Release{
				Spec: shipper.ReleaseSpec{
					TargetStep: 2,
				},
			},
			succ: nil,
			strategy: &shipper.RolloutStrategy{
				Steps: []shipper.RolloutStrategyStep{
					{},
					{},
					{},
				},
			},
			stepsFromAchievedStep: 2,
			progressing:           true,
			achievedTargetStep:    false,
			expectedTargetStep:    1,
			expectedError:         nil,
		},
		{
			title: "current non progressing release two steps target step",
			rel: &shipper.Release{
				Spec: shipper.ReleaseSpec{
					TargetStep: 0,
				},
			},
			succ: nil,
			strategy: &shipper.RolloutStrategy{
				Steps: []shipper.RolloutStrategyStep{
					{},
					{},
					{},
				},
			},
			stepsFromAchievedStep: 2,
			progressing:           false,
			achievedTargetStep:    false,
			expectedTargetStep:    1,
			expectedError:         nil,
		},
		{
			title: "successor progressing release one step target step",
			rel:   nil,
			succ: &shipper.Release{
				Spec: shipper.ReleaseSpec{
					TargetStep: 0,
				},
			},
			strategy: &shipper.RolloutStrategy{
				Steps: []shipper.RolloutStrategyStep{
					{},
				},
			},
			stepsFromAchievedStep: 1,
			progressing:           true,
			achievedTargetStep:    true,
			expectedTargetStep:    0,
			expectedError:         nil,
		},
		{
			title: "current progressing release one step target step with error",
			rel: &shipper.Release{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-release-0",
					Namespace: "test-namespace",
				},
				Spec: shipper.ReleaseSpec{
					TargetStep: 3,
				},
			},
			succ: nil,
			strategy: &shipper.RolloutStrategy{
				Steps: []shipper.RolloutStrategyStep{
					{},
				},
			},
			stepsFromAchievedStep: 1,
			progressing:           true,
			achievedTargetStep:    true,
			expectedTargetStep:    0,
			expectedError: shippererrors.NewUnrecoverableError(fmt.Errorf("no step %d in strategy for Release %q",
				3, "test-namespace/test-release-0")),
		},
	}

	for _, test := range tests {
		actual, err := GetTargetStep(test.rel, test.succ, test.strategy, test.stepsFromAchievedStep, test.progressing, test.achievedTargetStep)
		if !reflect.DeepEqual(err, test.expectedError) {
			t.Fatalf("expected error %v and got %v", test.expectedError, err)
		}
		if actual != test.expectedTargetStep {
			t.Fatalf("testing %s: expected target step %d and got %d", test.title, test.expectedTargetStep, actual)
		}
	}
}

func TestGetProcessedTargetStep(t *testing.T) {
	var tests = []struct {
		title                     string
		virtualStrategy           *shipper.RolloutVirtualStrategy
		targetStep                int32
		progressing               bool
		achievedTargetStep        bool
		expectedProcessTargetStep int32
	}{
		{
			title: "process step is same as target step (release progressing regularly)",
			virtualStrategy: &shipper.RolloutVirtualStrategy{
				Steps: []shipper.RolloutStrategyVirtualStep{
					{VirtualSteps: []shipper.RolloutStrategyStep{{}}},
				},
			},
			targetStep:                0,
			progressing:               true,
			achievedTargetStep:        false,
			expectedProcessTargetStep: 0,
		},
		{
			title: "process step is target step + 1 (release is going backwards)",
			virtualStrategy: &shipper.RolloutVirtualStrategy{
				Steps: []shipper.RolloutStrategyVirtualStep{
					{VirtualSteps: []shipper.RolloutStrategyStep{{}}},
					{VirtualSteps: []shipper.RolloutStrategyStep{{}}},
				},
			},
			targetStep:                0,
			progressing:               false,
			achievedTargetStep:        false,
			expectedProcessTargetStep: 1,
		},
		{
			title: "process step is same as target step (release has achieved target step)",
			virtualStrategy: &shipper.RolloutVirtualStrategy{
				Steps: []shipper.RolloutStrategyVirtualStep{
					{VirtualSteps: []shipper.RolloutStrategyStep{{}}},
				},
			},
			targetStep:                0,
			progressing:               true,
			achievedTargetStep:        true,
			expectedProcessTargetStep: 0,
		}, {
			title: "process step is different than target step (virtual strategy has less steps)",
			virtualStrategy: &shipper.RolloutVirtualStrategy{
				Steps: []shipper.RolloutStrategyVirtualStep{
					{VirtualSteps: []shipper.RolloutStrategyStep{{}}},
				},
			},
			targetStep:                1,
			progressing:               true,
			achievedTargetStep:        false,
			expectedProcessTargetStep: 0,
		},
	}

	for _, test := range tests {
		actual := GetProcessedTargetStep(test.virtualStrategy, test.targetStep, test.progressing, test.achievedTargetStep)
		if actual != test.expectedProcessTargetStep {
			t.Fatalf("testing %s: expected process target step %d and got %d", test.title, test.expectedProcessTargetStep, actual)
		}
	}
}

func TestGetVirtualTargetStep(t *testing.T) {
	var tests = []struct {
		title                     string
		rel                       *shipper.Release
		succ                      *shipper.Release
		virtualStrategy           *shipper.RolloutVirtualStrategy
		targetStep                int32
		processTargetStep         int32
		progressing               bool
		achievedTargetStep        bool
		expectedVirtualTargetStep int32
	}{
		{
			title: "release is contender, rollout moving forward, target step was bumped",
			rel: &shipper.Release{
				Spec: shipper.ReleaseSpec{
					TargetVirtualStep: 1,
				},
				Status: shipper.ReleaseStatus{
					AchievedVirtualStep: &shipper.AchievedVirtualStep{
						Step: 0,
					},
				},
			},
			succ: nil,
			virtualStrategy: &shipper.RolloutVirtualStrategy{
				Steps: []shipper.RolloutStrategyVirtualStep{
					{
						VirtualSteps: []shipper.RolloutStrategyStep{{}, {}},
					},
					{
						VirtualSteps: []shipper.RolloutStrategyStep{{}, {}},
					},
				},
			},
			targetStep:                1,
			processTargetStep:         1,
			progressing:               true,
			achievedTargetStep:        false,
			expectedVirtualTargetStep: 0,
		},
		{
			title: "release is contender, rollout moving forward",
			rel: &shipper.Release{
				Spec: shipper.ReleaseSpec{
					TargetVirtualStep: 1,
				},
				Status: shipper.ReleaseStatus{
					AchievedVirtualStep: &shipper.AchievedVirtualStep{
						Step: 0,
					},
				},
			},
			succ: nil,
			virtualStrategy: &shipper.RolloutVirtualStrategy{
				Steps: []shipper.RolloutStrategyVirtualStep{
					{
						VirtualSteps: []shipper.RolloutStrategyStep{{}, {}},
					},
					{
						VirtualSteps: []shipper.RolloutStrategyStep{{}, {}},
					},
				},
			},
			targetStep:                0,
			processTargetStep:         0,
			progressing:               true,
			achievedTargetStep:        false,
			expectedVirtualTargetStep: 1,
		},
		{
			title: "release is contender, rollout moving backwards, target step was bumped",
			rel: &shipper.Release{
				Spec: shipper.ReleaseSpec{
					TargetVirtualStep: 1,
				},
				Status: shipper.ReleaseStatus{
					AchievedVirtualStep: &shipper.AchievedVirtualStep{
						Step: 1,
					},
				},
			},
			succ: nil,
			virtualStrategy: &shipper.RolloutVirtualStrategy{
				Steps: []shipper.RolloutStrategyVirtualStep{
					{
						VirtualSteps: []shipper.RolloutStrategyStep{{}, {}},
					},
					{
						VirtualSteps: []shipper.RolloutStrategyStep{{}, {}},
					},
				},
			},
			targetStep:                0,
			processTargetStep:         0,
			progressing:               false,
			achievedTargetStep:        false,
			expectedVirtualTargetStep: 1,
		},
		{
			title: "release is incumbent, rollout moving forward",
			rel: &shipper.Release{
				Spec: shipper.ReleaseSpec{
					TargetVirtualStep: 2,
				},
				Status: shipper.ReleaseStatus{
					AchievedVirtualStep: &shipper.AchievedVirtualStep{
						Step: 0,
					},
				},
			},
			succ: &shipper.Release{
				Spec: shipper.ReleaseSpec{
					TargetVirtualStep: 0,
				},
				Status: shipper.ReleaseStatus{
					AchievedVirtualStep: &shipper.AchievedVirtualStep{
						Step: 0,
					},
				},
			},
			virtualStrategy: &shipper.RolloutVirtualStrategy{
				Steps: []shipper.RolloutStrategyVirtualStep{
					{
						VirtualSteps: []shipper.RolloutStrategyStep{{}, {}},
					},
					{
						VirtualSteps: []shipper.RolloutStrategyStep{{}, {}},
					},
				},
			},
			targetStep:                1,
			processTargetStep:         1,
			progressing:               true,
			achievedTargetStep:        false,
			expectedVirtualTargetStep: 0,
		},
		{
			title: "release is incumbent, rollout moving forward, target step out of bound",
			rel: &shipper.Release{
				Spec: shipper.ReleaseSpec{
					TargetVirtualStep: 2,
				},
				Status: shipper.ReleaseStatus{
					AchievedVirtualStep: &shipper.AchievedVirtualStep{
						Step: 0,
					},
				},
			},
			succ: &shipper.Release{
				Spec: shipper.ReleaseSpec{
					TargetVirtualStep: 3,
				},
				Status: shipper.ReleaseStatus{
					AchievedVirtualStep: &shipper.AchievedVirtualStep{
						Step: 0,
					},
				},
			},
			virtualStrategy: &shipper.RolloutVirtualStrategy{
				Steps: []shipper.RolloutStrategyVirtualStep{
					{
						VirtualSteps: []shipper.RolloutStrategyStep{{}, {}},
					},
					{
						VirtualSteps: []shipper.RolloutStrategyStep{{}, {}},
					},
				},
			},
			targetStep:                1,
			processTargetStep:         1,
			progressing:               true,
			achievedTargetStep:        false,
			expectedVirtualTargetStep: 1,
		},
		{
			title: "release is contender, has achieved target step",
			rel: &shipper.Release{
				Spec: shipper.ReleaseSpec{
					TargetVirtualStep: 1,
				},
				Status: shipper.ReleaseStatus{
					AchievedVirtualStep: &shipper.AchievedVirtualStep{
						Step: 1,
					},
				},
			},
			succ: nil,
			virtualStrategy: &shipper.RolloutVirtualStrategy{
				Steps: []shipper.RolloutStrategyVirtualStep{
					{
						VirtualSteps: []shipper.RolloutStrategyStep{{}, {}},
					},
					{
						VirtualSteps: []shipper.RolloutStrategyStep{{}, {}},
					},
				},
			},
			targetStep:                1,
			processTargetStep:         1,
			progressing:               false,
			achievedTargetStep:        true,
			expectedVirtualTargetStep: 1,
		},
	}

	for _, test := range tests {
		actual := GetVirtualTargetStep(test.rel, test.succ, test.virtualStrategy, test.targetStep, test.processTargetStep, test.progressing, test.achievedTargetStep)
		if actual != test.expectedVirtualTargetStep {
			t.Fatalf("testing %s: expected virtual target step %d and got %d", test.title, test.expectedVirtualTargetStep, actual)
		}
	}
}
