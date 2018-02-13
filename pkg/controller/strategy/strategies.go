package strategy

import "github.com/bookingcom/shipper/pkg/apis/shipper/v1"

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
			ContenderCapacity: "0",
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

func getStrategy(name string) *v1.StrategySpec {
	if s, ok := strategies[name]; !ok {
		return allInStrategy
	} else {
		return s
	}
}
