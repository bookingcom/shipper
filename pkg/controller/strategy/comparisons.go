package strategy

func contenderTrafficComparison(achieved uint32, desired uint32) bool {
	return achieved >= desired
}

func incumbentTrafficComparison(achieved uint32, desired uint32) bool {
	return achieved <= desired
}
