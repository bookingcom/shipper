package release

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	apputil "github.com/bookingcom/shipper/pkg/util/application"
	"github.com/bookingcom/shipper/pkg/util/filters"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

func FilterSelectedClusters(selectedClusters []string, clustersToRemove []string) []string {
	var filteredClusters []string
	for _, selectedCluster := range selectedClusters {
		if !filters.SliceContainsString(clustersToRemove, selectedCluster) {
			filteredClusters = append(filteredClusters, selectedCluster)
		}
	}
	return filteredClusters
}

func IsContender(rel *shipper.Release, shipperClient shipperclientset.Interface) (bool, error) {
	appName := rel.Labels[shipper.AppLabel]
	app, err := shipperClient.ShipperV1alpha1().Applications(rel.Namespace).Get(appName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	contender, err := GetContender(app, shipperClient)
	if err != nil {
		return false, err
	}
	return contender.Name == rel.Name && contender.Namespace == rel.Namespace, nil
}

func IsIncumbent(rel *shipper.Release, shipperClient shipperclientset.Interface) (bool, error) {
	appName := rel.Labels[shipper.AppLabel]
	app, err := shipperClient.ShipperV1alpha1().Applications(rel.Namespace).Get(appName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	incumbent, err := GetIncumbent(app, shipperClient)
	if err != nil {
		return false, err
	}
	return incumbent.Name == rel.Name && incumbent.Namespace == rel.Namespace, nil
}

func GetContender(app *shipper.Application, shipperClient shipperclientset.Interface) (*shipper.Release, error) {
	releaseList, err := ReleasesForApplication(app.Name, app.Namespace, shipperClient)
	if err != nil {
		return nil, err
	}
	rels := make([]*shipper.Release, len(releaseList.Items))
	for i, _ := range releaseList.Items {
		rels[i] = &releaseList.Items[i]
	}
	rels = releaseutil.SortByGenerationDescending(rels)
	contender, err := apputil.GetContender(app.Name, rels)
	if err != nil {
		return nil, err
	}
	return contender, nil
}

func GetIncumbent(app *shipper.Application, shipperClient shipperclientset.Interface) (*shipper.Release, error) {
	releaseList, err := ReleasesForApplication(app.Name, app.Namespace, shipperClient)
	if err != nil {
		return nil, err
	}
	rels := make([]*shipper.Release, len(releaseList.Items))
	for i, _ := range releaseList.Items {
		rels[i] = &releaseList.Items[i]
	}
	rels = releaseutil.SortByGenerationDescending(rels)
	incumbent, err := apputil.GetIncumbent(app.Name, rels)
	if err != nil {
		return nil, err
	}
	return incumbent, nil
}

func ReleasesForApplication(appName, appNamespace string, shipperClient shipperclientset.Interface) (*shipper.ReleaseList, error) {
	selector := labels.Set{shipper.AppLabel: appName}.AsSelector()
	releaseList, err := shipperClient.ShipperV1alpha1().Releases(appNamespace).List(metav1.ListOptions{LabelSelector: selector.String()})
	return releaseList, err
}
