package strategy

import (
	"fmt"

	"github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

var allInStrategy = &v1.StrategySpec{
	Steps: []v1.StrategyStep{
		{
			ContenderCapacity: "0",
			IncumbentCapacity: "100",
			ContenderTraffic:  "0",
			IncumbentTraffic:  "100",
		},
		{
			ContenderCapacity: "100",
			IncumbentCapacity: "0",
			ContenderTraffic:  "100",
			IncumbentTraffic:  "0",
		},
	},
}

var vanguardStrategy = &v1.StrategySpec{
	Steps: []v1.StrategyStep{
		{
			ContenderCapacity: "1",
			IncumbentCapacity: "100",
			ContenderTraffic:  "0",
			IncumbentTraffic:  "100",
		},
		{
			ContenderCapacity: "50",
			IncumbentCapacity: "50",
			ContenderTraffic:  "50",
			IncumbentTraffic:  "50",
		},
		{
			ContenderCapacity: "100",
			IncumbentCapacity: "0",
			ContenderTraffic:  "100",
			IncumbentTraffic:  "0",
		},
	},
}

var strategies = map[string]*v1.StrategySpec{
	"allIn":    allInStrategy,
	"vanguard": vanguardStrategy,
}

func getStrategy(name string) *v1.Strategy {
	strat := &v1.Strategy{}
	if spec, ok := strategies[name]; !ok {
		strat.Name = "allIn"
		strat.Spec = *allInStrategy
		return strat
	} else {
		strat.Name = name
		strat.Spec = *spec
		return strat
	}
}

func getStrategyStep(s *v1.Strategy, idx int) (v1.StrategyStep, error) {
	if idx >= len(s.Spec.Steps) {
		return v1.StrategyStep{}, fmt.Errorf("no step %d in strategy %q", idx, s.Name)
	}
	return s.Spec.Steps[idx], nil
}
