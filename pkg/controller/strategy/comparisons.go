package strategy

func contenderCapacityComparison(achievedPercentage, desiredPercentage, totalNumberOfPods uint) bool {
	// Consider that contender capacity has achieved its desired state if
	// the end game is one pod and we already have it, as long as desired
	// is greater than zero.
	if totalNumberOfPods == 1 && achievedPercentage == 100 && desiredPercentage > 0 {
		return true
	}
	return achievedPercentage >= desiredPercentage
}

func incumbentCapacityComparison(achievedPercentage, desiredPercentage, totalNumberOfPods uint) bool {
	// Consider that the incumbent capacity has achieved its desired state if
	// the end game is one pod and we already have it, as long as the desired
	// is smaller than 100.
	if totalNumberOfPods == 1 && achievedPercentage == 100 && desiredPercentage < 100 {
		return true
	}
	return achievedPercentage <= desiredPercentage
}

func contenderTrafficComparison(achieved uint32, desired uint32) bool {
	return achieved >= desired
}

func incumbentTrafficComparison(achieved uint32, desired uint32) bool {
	return achieved <= desired
}
