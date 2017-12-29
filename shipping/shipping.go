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

// ShipmentRequest is...
type ShipmentRequest struct {
	APIVersion string                 `json:"apiVersion"`
	Kind       string                 `json:"kind"`
	Meta       ShipmentRequestMeta    `json:"meta"`
	Spec       map[string]interface{} `json:"spec,omitempty"`
}

func Ship(appName string, shipmentRequest *ShipmentRequest, accessToken string) error {

	s := &Shipper{
		ValidateAccessToken: func(accessToken string, appName string) error {
			return nil
		},
		ValidateApp: func(appName string) error {
			return nil
		},
		ValidateChart: func(chart Chart) error {
			return nil
		},
		ValidateImage: func(repository string, label string) error {
			return nil
		},
		PersistShipment: func(request *ShipmentRequest) error {
			return nil
		},
		FilterClusters: func(selectors []string) []models.Cluster {
			return []models.Cluster{
				{
					Name: "cluster-1",
				},
			}
		},
	}

	return s.Ship(appName, shipmentRequest, accessToken)
}
