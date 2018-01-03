package shipping

import (
	"github.com/bookingcom/shipper/models"
)

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
status:
  selectedClusters:
  - cluster-1
spec:
  perl:
	image:
	  repository: 'myapp'
	  tag: 'latest'

*/

// ShipmentStrategy is...
type ShipmentStrategy struct {
	Name string                 `json:"name"`
	Spec map[string]interface{} `json:"spec,omitempty"`
}

// Chart is...
type Chart struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ShipmentRequestMeta is...
type ShipmentRequestMeta struct {
	ClusterSelectors []string         `json:"clusterSelectors"`
	Chart            Chart            `json:"chart"`
	Strategy         ShipmentStrategy `json:"strategy"`
}

type ShipmentRequestStatus struct {
	SelectedClusters []string `json:"selectedClusters"`
}

// ShipmentRequest is...
type ShipmentRequest struct {
	APIVersion string                 `json:"apiVersion"`
	Kind       string                 `json:"kind"`
	Meta       ShipmentRequestMeta    `json:"meta"`
	Status     ShipmentRequestStatus  `json:"status,omitempty"`
	Spec       map[string]interface{} `json:"spec,omitempty"`
}

func validateAccessToken(accessToken string, appName string) error {
	return nil
}

func validateChart(chart Chart) error {
	return nil
}

func validateApp(appName string) error {
	return nil
}

func validateImage(repository string, label string) error {
	return nil
}

func persistShipment(request *ShipmentRequest) error {
	return nil
}

func filterClusters(selectors []string) []models.Cluster {
	return []models.Cluster{
		{
			Name: "cluster-1",
		},
	}
}

func renderChart(request *ShipmentRequest, clusterName string) ([]string, error) {
	return []string{
		"Kubernetes Object",
	}, nil
}

func Ship(appName string, shipmentRequest *ShipmentRequest, accessToken string) error {

	s := &Shipper{
		ValidateAccessToken: validateAccessToken,
		ValidateApp:         validateApp,
		ValidateChart:       validateChart,
		ValidateImage:       validateImage,
		PersistShipment:     persistShipment,
		FilterClusters:      filterClusters,
		RenderChart:         renderChart,
	}

	return s.Ship(appName, shipmentRequest, accessToken)
}
