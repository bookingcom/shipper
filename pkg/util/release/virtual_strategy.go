package release

import (
	"fmt"
	"math"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func BuildVirtualStrategy(strategy *shipper.RolloutStrategy, surgeWeight int) (*shipper.RolloutVirtualStrategy, error) {
	rolloutVirtualStrategy := make([]shipper.RolloutStrategyVirtualStep, len(strategy.Steps))

	// create the first virtual transition: from previous release to first strategy step
	step := shipper.RolloutStrategyStep{
		Name: "",
		Capacity: shipper.RolloutStrategyStepValue{
			Incumbent: 100,
			Contender: 0,
		},
		Traffic: shipper.RolloutStrategyStepValue{
			Incumbent: 100,
			Contender: 0,
		},
	}
	nextStep := strategy.Steps[0]
	rolloutStrategyVirtualSteps := buildRolloutStrategyVirtualSteps(step, nextStep, surgeWeight)
	rolloutVirtualStrategy[0] = shipper.RolloutStrategyVirtualStep{VirtualSteps: rolloutStrategyVirtualSteps}

	for i, step := range strategy.Steps {
		if i != len(strategy.Steps)-1 {
			nextStep := strategy.Steps[i+1]
			rolloutStrategyVirtualSteps := buildRolloutStrategyVirtualSteps(step, nextStep, surgeWeight)

			rolloutVirtualStrategy[i+1] = shipper.RolloutStrategyVirtualStep{VirtualSteps: rolloutStrategyVirtualSteps}
		}
	}
	virtualStrategy := &shipper.RolloutVirtualStrategy{Steps: rolloutVirtualStrategy}
	return virtualStrategy, nil
}

func buildRolloutStrategyVirtualSteps(step, nextStep shipper.RolloutStrategyStep, surgeWeight int) []shipper.RolloutStrategyStep {
	// Calculate virtual steps
	// Contender:
	currContenderCapacity := step.Capacity.Contender
	currContenderTraffic := step.Traffic.Contender
	nextContenderCapacity := nextStep.Capacity.Contender
	nextContenderTraffic := nextStep.Traffic.Contender
	diffContenderCapacity := float64(nextContenderCapacity - currContenderCapacity)
	virtualStepsContender := math.Ceil(diffContenderCapacity / float64(surgeWeight))

	// Incumbent:
	currIncumbentCapacity := step.Capacity.Incumbent
	currIncumbentTraffic := step.Traffic.Incumbent
	nextIncumbentCapacity := nextStep.Capacity.Incumbent
	nextIncumbentTraffic := nextStep.Traffic.Incumbent
	diffIncumbentCapacity := float64(currIncumbentCapacity - nextIncumbentCapacity)
	virtualStepsIncumbent := math.Ceil(diffIncumbentCapacity / float64(surgeWeight))

	virtualSteps := math.Max(virtualStepsContender, virtualStepsIncumbent)
	virtualSteps = math.Max(virtualSteps, 1)

	// Calculate traffic surge to each step
	diffContenderTraffic := float64(nextContenderTraffic - currContenderTraffic)
	diffIncumbentTraffic := float64(nextIncumbentTraffic - currIncumbentTraffic)
	trafficSurgeContender := math.Ceil(diffContenderTraffic / virtualSteps)
	trafficSurgeIncumbent := math.Ceil(diffIncumbentTraffic / virtualSteps)
	trafficSurge := int(math.Max(trafficSurgeContender, trafficSurgeIncumbent))

	rolloutStrategyVirtualSteps := make([]shipper.RolloutStrategyStep, int(virtualSteps)+1)
	for j, _ := range rolloutStrategyVirtualSteps {
		multiplier := j // + 1
		// Contender:
		// Capacity
		possibleContenderCapacity := currContenderCapacity + int32(multiplier*surgeWeight)
		newContenderCapacity := int32(math.Min(float64(possibleContenderCapacity), float64(nextContenderCapacity)))
		// Traffic
		possibleContenderTraffic := currContenderTraffic + int32(multiplier*trafficSurge)
		newContenderTraffic := int32(math.Min(float64(possibleContenderTraffic), float64(nextContenderTraffic)))

		// Incumbent:
		// Capacity
		possibleIncumbentCapacity := currIncumbentCapacity - int32(multiplier*surgeWeight)
		newIncumbentCapacity := int32(math.Max(float64(possibleIncumbentCapacity), float64(nextIncumbentCapacity)))
		// Traffic
		possibleIncumbentTraffic := currIncumbentTraffic - int32(multiplier*trafficSurge)
		newIncumbentTraffic := int32(math.Max(float64(possibleIncumbentTraffic), float64(nextIncumbentTraffic)))

		rolloutStrategyVirtualSteps[j] = shipper.RolloutStrategyStep{
			Name: fmt.Sprintf("%d: %s to %s", j, step.Name, nextStep.Name),
			Capacity: shipper.RolloutStrategyStepValue{
				Incumbent: newIncumbentCapacity,
				Contender: newContenderCapacity,
			},
			Traffic: shipper.RolloutStrategyStepValue{
				Incumbent: newIncumbentTraffic,
				Contender: newContenderTraffic,
			},
		}
	}
	return rolloutStrategyVirtualSteps
}
