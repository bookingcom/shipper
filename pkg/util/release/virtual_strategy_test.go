package release

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/pmezard/go-difflib/difflib"
	"sigs.k8s.io/yaml"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func TestBuildVirtualStrategy(t *testing.T) {
	var tests = []struct {
		title                          string
		maxSurge                       int
		strategy                       *shipper.RolloutStrategy
		expectedRolloutVirtualStrategy *shipper.RolloutVirtualStrategy
	}{
		{
			"test building virtual strategy with maxSurge=30% (feature on)",
			30,
			&shipper.RolloutStrategy{
				Steps: []shipper.RolloutStrategyStep{
					{
						Name:     "staging",
						Capacity: shipper.RolloutStrategyStepValue{Incumbent: 100, Contender: 1},
						Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 100, Contender: 0},
					},
					{
						Name:     "50/50",
						Capacity: shipper.RolloutStrategyStepValue{Incumbent: 50, Contender: 50},
						Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 50, Contender: 50},
					},
					{
						Name:     "full on",
						Capacity: shipper.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
						Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
					},
				},
			},
			&shipper.RolloutVirtualStrategy{
				Steps: []shipper.RolloutStrategyVirtualStep{
					{
						VirtualSteps: []shipper.RolloutStrategyStep{
							{
								Capacity: shipper.RolloutStrategyStepValue{
									Incumbent: 100,
									Contender: 0,
								},
								Traffic: shipper.RolloutStrategyStepValue{
									Incumbent: 100,
									Contender: 0,
								},
							},
							{
								Capacity: shipper.RolloutStrategyStepValue{
									Incumbent: 100,
									Contender: 1,
								},
								Traffic: shipper.RolloutStrategyStepValue{
									Incumbent: 100,
									Contender: 0,
								},
							},
						},
					},
					{
						VirtualSteps: []shipper.RolloutStrategyStep{
							{
								Capacity: shipper.RolloutStrategyStepValue{
									Incumbent: 100,
									Contender: 1,
								},
								Traffic: shipper.RolloutStrategyStepValue{
									Incumbent: 100,
									Contender: 0,
								},
							},
							{
								Capacity: shipper.RolloutStrategyStepValue{
									Incumbent: 70,
									Contender: 31,
								},
								Traffic: shipper.RolloutStrategyStepValue{
									Incumbent: 75,
									Contender: 25,
								},
							},
							{
								Capacity: shipper.RolloutStrategyStepValue{
									Incumbent: 50,
									Contender: 50,
								},
								Traffic: shipper.RolloutStrategyStepValue{
									Incumbent: 50,
									Contender: 50,
								},
							},
						},
					},
					{
						VirtualSteps: []shipper.RolloutStrategyStep{
							{
								Capacity: shipper.RolloutStrategyStepValue{
									Incumbent: 50,
									Contender: 50,
								},
								Traffic: shipper.RolloutStrategyStepValue{
									Incumbent: 50,
									Contender: 50,
								},
							},
							{
								Capacity: shipper.RolloutStrategyStepValue{
									Incumbent: 20,
									Contender: 80,
								},
								Traffic: shipper.RolloutStrategyStepValue{
									Incumbent: 25,
									Contender: 75,
								},
							},
							{
								Capacity: shipper.RolloutStrategyStepValue{
									Incumbent: 0,
									Contender: 100,
								},
								Traffic: shipper.RolloutStrategyStepValue{
									Incumbent: 0,
									Contender: 100,
								},
							},
						},
					},
				},
			},
		},
		{
			"test building virtual strategy with maxSurge=100%, default value (feature off)",
			100,
			&shipper.RolloutStrategy{
				Steps: []shipper.RolloutStrategyStep{
					{
						Name:     "staging",
						Capacity: shipper.RolloutStrategyStepValue{Incumbent: 100, Contender: 1},
						Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 100, Contender: 0},
					},
					{
						Name:     "50/50",
						Capacity: shipper.RolloutStrategyStepValue{Incumbent: 50, Contender: 50},
						Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 50, Contender: 50},
					},
					{
						Name:     "full on",
						Capacity: shipper.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
						Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
					},
				},
			},
			&shipper.RolloutVirtualStrategy{
				Steps: []shipper.RolloutStrategyVirtualStep{
					{
						VirtualSteps: []shipper.RolloutStrategyStep{
							{
								Capacity: shipper.RolloutStrategyStepValue{
									Incumbent: 100,
									Contender: 0,
								},
								Traffic: shipper.RolloutStrategyStepValue{
									Incumbent: 100,
									Contender: 0,
								},
							},
							{
								Capacity: shipper.RolloutStrategyStepValue{
									Incumbent: 100,
									Contender: 1,
								},
								Traffic: shipper.RolloutStrategyStepValue{
									Incumbent: 100,
									Contender: 0,
								},
							},
						},
					},
					{
						VirtualSteps: []shipper.RolloutStrategyStep{
							{
								Capacity: shipper.RolloutStrategyStepValue{
									Incumbent: 100,
									Contender: 1,
								},
								Traffic: shipper.RolloutStrategyStepValue{
									Incumbent: 100,
									Contender: 0,
								},
							},
							{
								Capacity: shipper.RolloutStrategyStepValue{
									Incumbent: 50,
									Contender: 50,
								},
								Traffic: shipper.RolloutStrategyStepValue{
									Incumbent: 50,
									Contender: 50,
								},
							},
						},
					},
					{
						VirtualSteps: []shipper.RolloutStrategyStep{
							{
								Capacity: shipper.RolloutStrategyStepValue{
									Incumbent: 50,
									Contender: 50,
								},
								Traffic: shipper.RolloutStrategyStepValue{
									Incumbent: 50,
									Contender: 50,
								},
							},
							{
								Capacity: shipper.RolloutStrategyStepValue{
									Incumbent: 0,
									Contender: 100,
								},
								Traffic: shipper.RolloutStrategyStepValue{
									Incumbent: 0,
									Contender: 100,
								},
							},
						},
					},
				},
			},
		},
	}
	for _, test := range tests {
		virtualStrategy, err := BuildVirtualStrategy(test.strategy, test.maxSurge)
		if err != nil {
			t.Fatal(err)
		}

		if equal, diff := DeepEqualDiff(test.expectedRolloutVirtualStrategy, virtualStrategy); !equal {
			t.Fatalf("%s\n%s", test.title, diff)
		}
	}
}

func YamlDiff(a interface{}, b interface{}) (string, error) {
	yamlActual, err := yaml.Marshal(a)
	if err != nil {
		return "", err
	}

	yamlExpected, err := yaml.Marshal(b)
	if err != nil {
		return "", err
	}

	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(yamlExpected)),
		B:        difflib.SplitLines(string(yamlActual)),
		FromFile: "Expected",
		ToFile:   "Actual",
		Context:  4,
	}

	return difflib.GetUnifiedDiffString(diff)
}

func DeepEqualDiff(expected, actual interface{}) (bool, string) {
	if !reflect.DeepEqual(actual, expected) {
		diff, err := YamlDiff(actual, expected)
		if err != nil {
			panic(fmt.Sprintf("couldn't generate yaml diff: %s", err))
		}

		return false, diff
	}

	return true, ""
}
