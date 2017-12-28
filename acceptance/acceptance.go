package acceptance

/*
apiVersion: shipper/v1
kind: ShipmentRequest
metadata:
  clusterSelectors:
  - pci
  - gpu
  chart:
    name: 'perl'
    version: '0.0.1'
  strategy:
	name: 'vanguard'
	spec:
	  stepCount: '10%'
	  initialReplicas: 5
spec:
  perl:
	image:
	  repository: 'myapp'
	  tag: 'latest'

*/

// ShipmentStrategy is...
type ShipmentStrategy struct {
	Name string
	Spec map[string]interface{}
}

// Chart is...
type Chart struct {
	Name    string
	Version string
}

// ShipmentRequestMeta is...
type ShipmentRequestMeta struct {
	ClusterSelectors []string
	Chart            Chart
	Strategy         ShipmentStrategy
}

// ShipmentRequest is...
type ShipmentRequest struct {
	APIVersion string
	Kind       string
	Meta       ShipmentRequestMeta
	Spec       map[string]interface{}
}

// CanAccessTokenShipApplication does...
func CanAccessTokenShipApplication(accessToken string, appName string) bool {
	return accessToken == "FOOBARBAZ"
}

// IsAppRegisteredInServiceDirectory does...
func IsAppRegisteredInServiceDirectory(name string) bool {
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
