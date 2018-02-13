package v1

//noinspection GoReceiverNames
func (s *StrategySpec) GetStep(idx uint) (StrategyStep, error) {
	return s.Steps[idx], nil
}
