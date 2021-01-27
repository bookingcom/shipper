package release

import (
	"k8s.io/apimachinery/pkg/types"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	"github.com/bookingcom/shipper/pkg/util/conditions"
)

type fakePipeline struct {
	steps     []string
}

func (p *fakePipeline) increase(ctx *context, firstRelInfo, secondRelInfo *releaseInfo) {
	p.Enqueue(genCapacityEnforcer(ctx, firstRelInfo, secondRelInfo))
	p.Enqueue(genTrafficEnforcer(ctx, firstRelInfo, secondRelInfo))
}

func (p *fakePipeline) decrease(ctx *context, firstRelInfo, secondRelInfo *releaseInfo) {
	p.Enqueue(genTrafficEnforcer(ctx, firstRelInfo, secondRelInfo))
	p.Enqueue(genCapacityEnforcer(ctx, firstRelInfo, secondRelInfo))
}

func (p *fakePipeline) Enqueue(step PipelineStep) {
	functionName := getFunctionName(step)
	p.steps = append(p.steps, functionName)
}

func getFunctionName(i interface{}) string {
	fullName := runtime.FuncForPC(reflect.ValueOf(i).Pointer()).Name()
	relativeName := filepath.Base(fullName)
	split := strings.Split(relativeName, ".")
	return split[1]
}

func (p *fakePipeline) Process(strategyStep shipper.RolloutStrategyStep, cond conditions.StrategyConditionsMap) (bool, []StrategyPatch, []ReleaseStrategyStateTransition) {
	return false, nil, nil
}

const testPrevRelease = "test-prev-release"
const testCurrRelease = "test-curr-release"
const testSuccRelease = "test-succ-release"
const testNamespace = "test-namespace"
const succRelUid = "random-succ-uid"
const currRelUid = "random-curr-uid"
const prevRelUid = "random-prev-uid"

func TestExecute(t *testing.T) {
	var tests = []struct {
		name                string
		prev, curr, succ    *releaseInfo
		isSteppingBackwards bool
		expectedOut         []string
	}{
		{
			name: "Progressing release, no history",
			prev: nil,
			curr: &releaseInfo{
				release: &shipper.Release{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testCurrRelease,
						Namespace: testNamespace,
						UID:       currRelUid,
					},
					Spec: shipper.ReleaseSpec{
						TargetStep: 0,
						Environment: shipper.ReleaseEnvironment{
							Strategy: &vanguard,
						},
					},
					Status: shipper.ReleaseStatus{},
				},
			},
			succ:                nil,
			isSteppingBackwards: false,
			expectedOut: []string{"genInstallationEnforcer","genCapacityEnforcer","genTrafficEnforcer","genReleaseStrategyStateEnforcer"},
		},
		{
			name: "Progressing release, one history",
			prev: fullOnRelInfo(testPrevRelease, prevRelUid),
			curr: &releaseInfo{
				release: &shipper.Release{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testCurrRelease,
						Namespace: testNamespace,
						UID:       currRelUid,
					},
					Spec: shipper.ReleaseSpec{
						TargetStep: 0,
						Environment: shipper.ReleaseEnvironment{
							Strategy: &vanguard,
						},
					},
					Status: shipper.ReleaseStatus{},
				},
			},
			succ:                nil,
			isSteppingBackwards: false,
			expectedOut: []string{"genInstallationEnforcer","genCapacityEnforcer","genTrafficEnforcer","genTrafficEnforcer","genCapacityEnforcer","genReleaseStrategyStateEnforcer"},
		},
		{
			name: "Stepping back release, no history",
			prev: nil,
			curr: &releaseInfo{
				release: &shipper.Release{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testCurrRelease,
						Namespace: testNamespace,
						UID:       currRelUid,
					},
					Spec: shipper.ReleaseSpec{
						TargetStep: 0,
						Environment: shipper.ReleaseEnvironment{
							Strategy: &vanguard,
						},
					},
					Status: shipper.ReleaseStatus{},
				},
			},
			succ:                nil,
			isSteppingBackwards: true,
			expectedOut:         []string{"genInstallationEnforcer","genTrafficEnforcer","genCapacityEnforcer","genReleaseStrategyStateEnforcer"},
		},
		{
			name: "Stepping back release, one history",
			prev: fullOnRelInfo(testPrevRelease, prevRelUid),
			curr: &releaseInfo{
				release: &shipper.Release{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testCurrRelease,
						Namespace: testNamespace,
						UID:       currRelUid,
					},
					Spec: shipper.ReleaseSpec{
						TargetStep: 0,
						Environment: shipper.ReleaseEnvironment{
							Strategy: &vanguard,
						},
					},
					Status: shipper.ReleaseStatus{},
				},
			},
			succ:                nil,
			isSteppingBackwards: true,
			expectedOut:         []string{"genInstallationEnforcer","genCapacityEnforcer","genTrafficEnforcer","genTrafficEnforcer","genCapacityEnforcer","genReleaseStrategyStateEnforcer"},
		},
		{
			name: "Succ is progressing, curr waits for succ",
			prev: fullOnRelInfo(testPrevRelease, prevRelUid),
			curr: fullOnRelInfo(testCurrRelease, currRelUid),
			succ: &releaseInfo{
				release: &shipper.Release{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testSuccRelease,
						Namespace: testNamespace,
						UID:       succRelUid,
					},
					Spec: shipper.ReleaseSpec{
						TargetStep: 1,
						Environment: shipper.ReleaseEnvironment{
							Strategy: &vanguard,
						},
					},
					Status: shipper.ReleaseStatus{},
				},
			},
			isSteppingBackwards: false,
			expectedOut:         []string{},
		},
		{
			name: "Succ achieved next step, curr reduces",
			prev: fullOnRelInfo(testPrevRelease, prevRelUid),
			curr: fullOnRelInfo(testCurrRelease, currRelUid),
			succ: &releaseInfo{
				release: &shipper.Release{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testSuccRelease,
						Namespace: testNamespace,
						UID:       succRelUid,
					},
					Spec: shipper.ReleaseSpec{
						TargetStep: 1,
						Environment: shipper.ReleaseEnvironment{
							Strategy: &vanguard,
						},
					},
					Status: shipper.ReleaseStatus{
						AchievedStep: &shipper.AchievedStep{
							Step: 1,
							Name: "50/50",
						},
					},
				},
			},
			isSteppingBackwards: false,
			expectedOut:         []string{"genInstallationEnforcer","genTrafficEnforcer","genCapacityEnforcer","genReleaseStrategyStateEnforcer"},
		},
		{
			name: "Succ is stepping back, curr waits for succ",
			prev: fullOnRelInfo(testPrevRelease, prevRelUid),
			curr: fullOnRelInfo(testCurrRelease, currRelUid),
			succ: &releaseInfo{
				release: &shipper.Release{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testSuccRelease,
						Namespace: testNamespace,
						UID:       succRelUid,
					},
					Spec: shipper.ReleaseSpec{
						TargetStep: 1,
						Environment: shipper.ReleaseEnvironment{
							Strategy: &vanguard,
						},
					},
					Status: shipper.ReleaseStatus{},
				},
			},
			isSteppingBackwards: true,
			expectedOut:         []string{},
		},
		{
			name: "Succ achieved previous step, curr increases",
			prev: fullOnRelInfo(testPrevRelease, prevRelUid),
			curr: fullOnRelInfo(testCurrRelease, currRelUid),
			succ: &releaseInfo{
				release: &shipper.Release{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testSuccRelease,
						Namespace: testNamespace,
						UID:       succRelUid,
					},
					Spec: shipper.ReleaseSpec{
						TargetStep: 1,
						Environment: shipper.ReleaseEnvironment{
							Strategy: &vanguard,
						},
					},
					Status: shipper.ReleaseStatus{
						AchievedStep: &shipper.AchievedStep{
							Step: 1,
							Name: "50/50",
						},
					},
				},
			},
			isSteppingBackwards: true,
			expectedOut:         []string{"genInstallationEnforcer","genCapacityEnforcer","genTrafficEnforcer","genReleaseStrategyStateEnforcer"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(tt *testing.T) {
			executor := NewStrategyExecutor(test.curr.release.Spec.Environment.Strategy, test.curr.release.Spec.TargetStep, test.isSteppingBackwards)
			// write  to buffer
			pipeline := &fakePipeline{
				steps:     []string{},
			}
			executor.Execute(test.prev, test.curr, test.succ, pipeline)

			eq, diff := shippertesting.DeepEqualDiff(test.expectedOut, pipeline.steps)
			if !eq {
				tt.Fatalf("pipeline execution differs from expected:\n%s", diff)
			}
		})
	}

}

func fullOnRelInfo(name, uidString string) *releaseInfo {
	uid := types.UID(uidString)
	return &releaseInfo{
		release: &shipper.Release{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNamespace,
				UID:       uid,
			},
			Spec: shipper.ReleaseSpec{
				TargetStep: 2,
				Environment: shipper.ReleaseEnvironment{
					Strategy: &vanguard,
				},
			},
			Status: shipper.ReleaseStatus{
				AchievedStep: &shipper.AchievedStep{
					Step: 2,
					Name: "full on",
				},
			},
		},
		installationTarget: &shipper.InstallationTarget{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNamespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						Name: name,
						UID:  uid,
					},
				},
			},
		},
		trafficTarget: &shipper.TrafficTarget{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNamespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						Name: name,
						UID:  uid,
					},
				},
			},
		},
		capacityTarget: &shipper.CapacityTarget{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNamespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						Name: name,
						UID:  uid,
					},
				},
			},
		},
	}
}
