package v1alpha1

import (
	"sort"

	"k8s.io/apimachinery/pkg/labels"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	apputil "github.com/bookingcom/shipper/pkg/util/application"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

type ReleaseListerExpansion interface{}

// ReleaseNamespaceListerExpansion allows custom methods to be added to
// ReleaseNamespaceLister.
type ReleaseNamespaceListerExpansion interface {
	// ReleasesForApplication returns Releases related to the given application
	// name ordered by generation.
	ReleasesForApplication(appName string) ([]*shipper.Release, error)

	// ContenderForApplication returns the contender Release for the given
	// application name.
	ContenderForApplication(appName string) (*shipper.Release, error)

	// IncumbentForApplication returns the incumbent Release for the given
	// application name.
	IncumbentForApplication(appName string) (*shipper.Release, error)
}

func (s releaseNamespaceLister) ReleasesForApplication(appName string) ([]*shipper.Release, error) {
	selector := labels.Set{shipper.AppLabel: appName}.AsSelector()
	selectedRels, err := s.List(selector)
	if err != nil {
		return nil, shippererrors.NewKubeclientListError(
			shipper.SchemeGroupVersion.WithKind("Release"),
			s.namespace, selector, err)
	}
	for _, e := range selectedRels {
		_, err := releaseutil.GetGeneration(e)
		if err != nil {
			return nil, err
		}
	}
	return selectedRels, nil
}

func (s releaseNamespaceLister) ContenderForApplication(appName string) (*shipper.Release, error) {
	rels, err := s.ReleasesForApplication(appName)
	if err != nil {
		return nil, err
	}
	sort.Sort(releaseutil.ByGenerationDescending(rels))
	return apputil.GetContender(appName, rels)
}

func (s releaseNamespaceLister) IncumbentForApplication(appName string) (*shipper.Release, error) {
	rels, err := s.ReleasesForApplication(appName)
	if err != nil {
		return nil, err
	}
	sort.Sort(releaseutil.ByGenerationDescending(rels))
	return apputil.GetIncumbent(appName, rels)
}
