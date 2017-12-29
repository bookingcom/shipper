package shipping

import (
	"fmt"
	"github.com/bookingcom/shipper/models"
	"testing"
)

//noinspection ALL
func invalidAccessToken(accessToken string, appName string) error {
	return fmt.Errorf("invalid access token")
}

//noinspection ALL
func validAccessToken(accessToken string, appName string) error {
	return nil
}

//noinspection ALL
func invalidApp(appName string) error {
	return fmt.Errorf("application not found in Service Directory")
}

//noinspection ALL
func validApp(appName string) error {
	return nil
}

//noinspection ALL
func invalidChart(chart Chart) error {
	return fmt.Errorf("chart not found in Helm repository")
}

//noinspection ALL
func validChart(chart Chart) error {
	return nil
}

//noinspection ALL
func invalidImage(repository string, label string) error {
	return fmt.Errorf("image not found in Docker repository")
}

//noinspection ALL
func validImage(repository string, label string) error {
	return nil
}

//noinspection ALL
func persistedShipment(request *ShipmentRequest) error {
	return nil
}

//noinspection ALL
func oneCluster(selectors []string) []models.Cluster {
	return []models.Cluster{
		{
			Name: "cluster-1",
		},
	}
}

//noinspection ALL
func noClusters(selectors []string) []models.Cluster {
	return []models.Cluster{}
}

//noinspection ALL
func renderChart(request *ShipmentRequest, clusterName string) ([]string, error) {
	return []string{
		"Kubernetes Object",
	}, nil
}

func newShipper() *Shipper {
	return &Shipper{
		ValidateAccessToken: validAccessToken,
		ValidateApp:         validApp,
		ValidateChart:       validChart,
		ValidateImage:       validImage,
		FilterClusters:      oneCluster,
		RenderChart:         renderChart,
		PersistShipment:     persistedShipment,
	}
}

func TestInvalidAccessToken(t *testing.T) {
	s := newShipper()
	s.ValidateAccessToken = invalidAccessToken

	appName := "my-app"
	accessToken := "Access Token"
	request := &ShipmentRequest{}
	err := s.Ship(appName, request, accessToken)
	if err == nil {
		t.Errorf("should return error, access token is invalid")
	}
}

func TestInvalidApp(t *testing.T) {
	s := newShipper()
	s.ValidateApp = invalidApp

	appName := "my-app"
	accessToken := "Access Token"
	request := &ShipmentRequest{}
	err := s.Ship(appName, request, accessToken)
	if err == nil {
		t.Errorf("should return error, application is invalid")
	}
}

func TestInvalidChart(t *testing.T) {
	s := newShipper()
	s.ValidateChart = invalidChart

	appName := "my-app"
	accessToken := "Access Token"
	request := &ShipmentRequest{}
	err := s.Ship(appName, request, accessToken)
	if err == nil {
		t.Errorf("should return error, chart is invalid")
	}
}

func TestInvalidImage(t *testing.T) {
	s := newShipper()
	s.ValidateImage = invalidImage

	appName := "my-app"
	accessToken := "Access Token"
	request := &ShipmentRequest{}
	err := s.Ship(appName, request, accessToken)
	if err == nil {
		t.Errorf("should return error, image is invalid")
	}
}

func TestFailRenderChart(t *testing.T) {
	s := newShipper()
	s.RenderChart = func(request *ShipmentRequest, clusterName string) ([]string, error) {
		return nil, fmt.Errorf("error rendering chart")
	}

	appName := "my-app"
	accessToken := "Access Token"
	request := &ShipmentRequest{}
	err := s.Ship(appName, request, accessToken)
	if err == nil {
		t.Errorf("should return error, error rendering chart")
	}
}

func TestNoClusters(t *testing.T) {
	s := newShipper()
	s.FilterClusters = noClusters

	appName := "my-app"
	accessToken := "Access Token"
	request := &ShipmentRequest{}
	err := s.Ship(appName, request, accessToken)
	if err == nil {
		t.Errorf("should return error, no clusters could be selected")
	}
}

func TestSuccessfulShipment(t *testing.T) {
	s := newShipper()
	s.PersistShipment = func(request *ShipmentRequest) error {
		if len(request.Status.SelectedClusters) == 0 {
			t.Errorf("request should have a selected cluster")
		}
		return nil
	}

	appName := "my-app"
	accessToken := "Access Token"
	request := &ShipmentRequest{}
	err := s.Ship(appName, request, accessToken)
	if err != nil {
		t.Errorf("%+v", err)
	}
}
