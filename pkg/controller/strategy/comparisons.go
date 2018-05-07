package strategy

func contenderCapacityComparison(achieved uint, desired uint) bool {
	return achieved >= desired
}

func incumbentCapacityComparison(achieved uint, desired uint) bool {
	return achieved <= desired
}

func contenderTrafficComparison(achieved uint32, desired uint32) bool {
	return achieved >= desired
}

func incumbentTrafficComparison(achieved uint32, desired uint32) bool {
	return achieved <= desired
}
