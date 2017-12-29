package shipping

import (
	"fmt"
	"github.com/bookingcom/shipper/models"
)

// ValidateAccessTokenFunc validates if the given accessToken can ship the given
// application name. If the given accessToken is not valid, or in the case it is
// but doesn't have the right permissions on the given application name, should
// return an error.
type ValidateAccessTokenFunc func(accessToken string, appName string) error

// ValidateAppFunc validates the given application name according to some
// parameters, return an error if the application is not valid (for example, the
// application doesn't exist in a central repository or can't be currently
// shipped).
type ValidateAppFunc func(appName string) error

// ValidateChartFunc validates the given chart according to some parameters,
// returning an error if the chart is not valid.
type ValidateChartFunc func(chart Chart) error

// ValidateImageFunc validates the image repository and label according to some
// parameters, returning an error if the image is not valid.
type ValidateImageFunc func(repository string, label string) error

// PersistShipmentFunc persists the augmented ShipmentRequest object, returning
// an error if there were any problems while persisting the object.
type PersistShipmentFunc func(request *ShipmentRequest) error

// FilterClustersFunc returns a list of Cluster objects based on the given
// cluster selectors.
type FilterClustersFunc func(selectors []string) []models.Cluster

// RenderChartFunc renders the chart specified by the ShipmentRequest, and
// returns a list of rendered Kubernetes manifests, or an error in the case the
// chart couldn't be rendered by some reason.
type RenderChartFunc func(request *ShipmentRequest, clusterName string) ([]string, error)

type Shipper struct {
	ValidateAccessToken ValidateAccessTokenFunc
	ValidateApp         ValidateAppFunc
	ValidateChart       ValidateChartFunc
	ValidateImage       ValidateImageFunc
	FilterClusters      FilterClustersFunc
	PersistShipment     PersistShipmentFunc
	RenderChart         RenderChartFunc
}

func (s *Shipper) Ship(appName string, shipmentRequest *ShipmentRequest, accessToken string) error {
	if err := s.ValidateAccessToken(accessToken, appName); err != nil {
		return err
	}

	/*
	 * - Check if appName exists in Service Directory
	 * - Check if appName is currently being shipped
	 */
	if err := s.ValidateApp(appName); err != nil {
		return err
	}

	if err := s.ValidateChart(shipmentRequest.Meta.Chart); err != nil {
		return err
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
	if err := s.ValidateImage(imageRepository, imageLabel); err != nil {
		return err
	}

	// Collect the names of all the matching clusters. If no names have been
	// collected, return an error
	var selectedClusterNames []string
	for _, e := range s.FilterClusters(shipmentRequest.Meta.ClusterSelectors) {
		selectedClusterNames = append(selectedClusterNames, e.Name)
	}
	if len(selectedClusterNames) == 0 {
		return fmt.Errorf("could not find clusters matching cluster selectors")
	}

	var objectsPerCluster = make(map[string][]string)
	for _, e := range selectedClusterNames {
		objects, err := s.RenderChart(shipmentRequest, e)
		if err != nil {
			return fmt.Errorf("couldn't render chart for '%s': %s", e, err)
		}
		objectsPerCluster[e] = objects
	}

	// Add the clusters to the request's status, and persist it
	shipmentRequest.Status.SelectedClusters = selectedClusterNames
	if err := s.PersistShipment(shipmentRequest); err != nil {
		return err
	}

	return nil
}
