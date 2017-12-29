package shipping

import (
	"fmt"
	"github.com/bookingcom/shipper/models"
	"testing"
)

func invalidAccessToken(accessToken string, appName string) error {
	return fmt.Errorf("invalid access token")
}

func validAccessToken(accessToken string, appName string) error {
	return nil
}

func invalidApp(appName string) error {
	return fmt.Errorf("application not found in Service Directory")
}

func validApp(appName string) error {
	return nil
}

func invalidChart(chart Chart) error {
	return fmt.Errorf("chart not found in Helm repository")
}

func validChart(chart Chart) error {
	return nil
}

func invalidImage(repository string, label string) error {
	return fmt.Errorf("image not found in Docker repository")
}

func validImage(repository string, label string) error {
	return nil
}

func persistedShipment(request *ShipmentRequest) error {
	return nil
}

func oneCluster(selectors []string) []models.Cluster {
	return []models.Cluster{
		{
			Name: "cluster-1",
		},
	}
}

func noClusters(selectors []string) []models.Cluster {
	return []models.Cluster{}
}

func TestInvalidAccessToken(t *testing.T) {
	s := &Shipper{
		ValidateAccessToken: invalidAccessToken,
		ValidateApp:         validApp,
		ValidateChart:       validChart,
		ValidateImage:       validImage,
		PersistShipment:     persistedShipment,
		FilterClusters:      oneCluster,
	}

	appName := "my-app"
	accessToken := "Access Token"
	request := &ShipmentRequest{}
	err := s.Ship(appName, request, accessToken)
	if err == nil {
		t.Errorf("should return error, access token is invalid")
	}
}

func TestInvalidApp(t *testing.T) {
	s := &Shipper{
		ValidateAccessToken: validAccessToken,
		ValidateApp:         invalidApp,
		ValidateChart:       validChart,
		ValidateImage:       validImage,
		PersistShipment:     persistedShipment,
		FilterClusters:      oneCluster,
	}

	appName := "my-app"
	accessToken := "Access Token"
	request := &ShipmentRequest{}
	err := s.Ship(appName, request, accessToken)
	if err == nil {
		t.Errorf("should return error, application is invalid")
	}
}

func TestInvalidChart(t *testing.T) {
	s := &Shipper{
		ValidateAccessToken: validAccessToken,
		ValidateApp:         validApp,
		ValidateChart:       invalidChart,
		ValidateImage:       validImage,
		PersistShipment:     persistedShipment,
		FilterClusters:      oneCluster,
	}

	appName := "my-app"
	accessToken := "Access Token"
	request := &ShipmentRequest{}
	err := s.Ship(appName, request, accessToken)
	if err == nil {
		t.Errorf("should return error, chart is invalid")
	}
}

func TestInvalidImage(t *testing.T) {
	s := &Shipper{
		ValidateAccessToken: validAccessToken,
		ValidateApp:         validApp,
		ValidateChart:       validChart,
		ValidateImage:       invalidImage,
		PersistShipment:     persistedShipment,
		FilterClusters:      oneCluster,
	}

	appName := "my-app"
	accessToken := "Access Token"
	request := &ShipmentRequest{}
	err := s.Ship(appName, request, accessToken)
	if err == nil {
		t.Errorf("should return error, image is invalid")
	}
}

func TestNoClusters(t *testing.T) {
	s := &Shipper{
		ValidateAccessToken: validAccessToken,
		ValidateApp:         validApp,
		ValidateChart:       validChart,
		ValidateImage:       validImage,
		PersistShipment:     persistedShipment,
		FilterClusters:      noClusters,
	}

	appName := "my-app"
	accessToken := "Access Token"
	request := &ShipmentRequest{}
	err := s.Ship(appName, request, accessToken)
	if err == nil {
		t.Errorf("should return error, no clusters could be selected")
	}
}

func TestSuccessfulShipment(t *testing.T) {
	s := &Shipper{
		ValidateAccessToken: validAccessToken,
		ValidateApp:         validApp,
		ValidateChart:       validChart,
		ValidateImage:       validImage,
		PersistShipment:     persistedShipment,
		FilterClusters:      oneCluster,
	}

	appName := "my-app"
	accessToken := "Access Token"
	request := &ShipmentRequest{}
	err := s.Ship(appName, request, accessToken)
	if err != nil {
		t.Errorf("%+v", err)
	}
}
