package v1

import (
	"sort"
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperController "github.com/bookingcom/shipper/pkg/controller"
)

// ReleaseListerExpansion allows custom methods to be added to
// ReleaseLister.
type ReleaseListerExpansion interface{}

// ReleaseNamespaceListerExpansion allows custom methods to be added to
// ReleaseNamespaceLister.
type ReleaseNamespaceListerExpansion interface {
	ReleasesForApplication(appName string) ([]*shipperV1.Release, error)
	ContenderForApplication(appName string) (*shipperV1.Release, error)
	ReleaseForInstallationTarget(it *shipperV1.InstallationTarget) (*shipperV1.Release, error)
}

// ReleasesForApplication returns Releases related to the given application
// name ordered by generation.
func (s releaseNamespaceLister) ReleasesForApplication(appName string) ([]*shipperV1.Release, error) {
	selector := labels.Set{shipperV1.AppLabel: appName}.AsSelector()
	selectedRels, err := s.List(selector)
	if err != nil {
		return nil, err
	}

	filteredRels := make([]*shipperV1.Release, 0, len(selectedRels))
	for _, rel := range selectedRels {
		if rel.DeletionTimestamp != nil {
			continue
		}
		filteredRels = append(filteredRels, rel)
	}

	sort.Slice(selectedRels, func(i, j int) bool {
		return selectedRels[i].Generation < selectedRels[j].Generation
	})

	return selectedRels, nil
}

// ContenderForApplication returns the contender Release for the given application name.
func (s releaseNamespaceLister) ContenderForApplication(appName string) (*shipperV1.Release, error) {
	rels, err := s.ReleasesForApplication(appName)
	if err != nil {
		return nil, err
	}
	if len(rels) == 0 {
		return nil, fmt.Errorf("no contender found for application %q", appName)
	}
	return rels[len(rels)-1], nil
}

// ReleaseForInstallationTarget returns the Release associated with given
// InstallationTarget. The relationship is established through owner
// references.
func (s releaseNamespaceLister) ReleaseForInstallationTarget(it *shipperV1.InstallationTarget) (*shipperV1.Release, error) {
	owner, err := extractOwnerReference(it.ObjectMeta)
	if err != nil {
		return nil, err
	}

	rel, err := s.Get(owner.Name)
	if err != nil {
		return nil, err
	}

	if rel.UID != owner.UID {
		return nil, shipperController.NewWrongOwnerReferenceError(it.Name, it.UID, rel.UID)
	}

	return rel, nil
}

// extractOwnerReference returns an owner reference for the given object meta,
// or an error in the case there are multiple or no owner references.
func extractOwnerReference(it metaV1.ObjectMeta) (*metaV1.OwnerReference, error) {
	if n := len(it.OwnerReferences); n != 1 {
		return nil, shipperController.NewMultipleOwnerReferencesError(it.Name, n)
	}
	return &it.OwnerReferences[0], nil
}
