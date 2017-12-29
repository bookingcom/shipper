package shipping

import (
	"github.com/bookingcom/shipper/acceptance"
	"fmt"
	"github.com/bookingcom/shipper/models"
	"github.com/bookingcom/shipper/preparation"
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
	if !acceptance.IsAppRegisteredInServiceDirectory(appName) {
		return fmt.Errorf("application is not registered in Service Directory")
	}

	if !acceptance.CanAccessTokenShipApplication(accessToken, appName) {
		return fmt.Errorf("access token can't ship this application")
	}

	if !acceptance.CanShipApplication(appName) {
		return fmt.Errorf("application can't be shipped now")
	}

	chart := shipmentRequest.Meta.Chart
	if !acceptance.DoesChartExistInChartRepository(chart.Name, chart.Version) {
		return fmt.Errorf("chart %s:%s does not exist in chart repository", chart.Name, chart.Version)
	}

	/*
	 * Since the image repository and label are part of the configuration of the
	 * chart (which can be arbitrary), how do check this prior deployment?
	 * Perhaps this check should be performed *after* we've rendered the chart,
	 * where we can infer the Deployment manifests and then make sure the images
	 * do exist?
	*/
	imageRepository := ""
	imageLabel := ""
	if !acceptance.DoesImageExistInRegistry(imageRepository, imageLabel) {
		return fmt.Errorf("image %s:%s does not exist in Docker registry", imageRepository, imageLabel)
	}

	// clusters should be fetched from the catalog of clusters. Perhaps from the Service Directory?
	clusters := []models.Cluster{}
	selectedClusters := preparation.SelectClusters(clusters, shipmentRequest.Meta.ClusterSelectors)
	if len(selectedClusters) == 0 {
		return fmt.Errorf("could not find clusters matching cluster selectors")
	}

	return fmt.Errorf("not implemented")
}
