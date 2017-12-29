package shipping

import (
	"github.com/bookingcom/shipper/models"
	"testing"
)

func TestShip(t *testing.T) {
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
		FilterClusters: func(selectors []string) []models.Cluster {
			return []models.Cluster{
				{
					Name: "cluster-1",
				},
			}
		},
	}

	appName := "my-app"
	accessToken := "Access Token"
	request := &ShipmentRequest{}
	err := s.Ship(appName, request, accessToken)
	if err != nil {
		t.Errorf("%+v", err)
	}
}
