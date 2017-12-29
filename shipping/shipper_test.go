package shipping

import "testing"

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
	}

	appName := "my-app"
	accessToken := "Access Token"
	request := &ShipmentRequest{}
	s.Ship(appName, request, accessToken)
}
