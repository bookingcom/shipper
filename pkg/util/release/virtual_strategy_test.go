package release

import (
	"testing"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	testutil "github.com/bookingcom/shipper/pkg/util/testing"
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
								Name: "0:  to staging",
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
								Name: "1:  to staging",
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
								Name: "0: staging to 50/50",
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
								Name: "1: staging to 50/50",
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
								Name: "2: staging to 50/50",
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
								Name: "0: 50/50 to full on",
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
								Name: "1: 50/50 to full on",
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
								Name: "2: 50/50 to full on",
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
								Name: "0:  to staging",
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
								Name: "1:  to staging",
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
								Name: "0: staging to 50/50",
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
								Name: "1: staging to 50/50",
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
								Name: "0: 50/50 to full on",
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
								Name: "1: 50/50 to full on",
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

		if equal, diff := testutil.DeepEqualDiff(test.expectedRolloutVirtualStrategy, virtualStrategy); !equal {
			t.Fatalf("%s\n%s", test.title, diff)
		}
	}
}
