package acceptance

// CanAccessTokenShipApplication does...
func CanAccessTokenShipApplication(accessToken string, appName string) bool {
	return accessToken == "FOOBARBAZ"
}

// IsAppRegisteredInServiceDirectory does...
func IsAppRegisteredInServiceDirectory(appName string) bool {
	return false
}

// DoesChartExistInChartRepository does...
func DoesChartExistInChartRepository(chart string, chartVersion string) bool {
	return false
}

// DoesImageExistInRegistry does...
func DoesImageExistInRegistry(repository string, label string) bool {
	return false
}

// CanShipApplication does...
func CanShipApplication(appName string) bool {
	return false
}