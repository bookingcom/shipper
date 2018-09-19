package v1alpha1

import (
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippercontroller "github.com/bookingcom/shipper/pkg/controller"
)

// ReleaseListerExpansion allows custom methods to be added to
// ReleaseLister.
type ReleaseListerExpansion interface{}

// ReleaseNamespaceListerExpansion allows custom methods to be added to
// ReleaseNamespaceLister.
type ReleaseNamespaceListerExpansion interface {
	ReleasesForApplication(appName string) ([]*shipper.Release, error)
	ContenderForApplication(appName string) (*shipper.Release, error)
	ReleaseForInstallationTarget(it *shipper.InstallationTarget) (*shipper.Release, error)
}

// ReleasesForApplication returns Releases related to the given application
// name ordered by generation.
func (s releaseNamespaceLister) ReleasesForApplication(appName string) ([]*shipper.Release, error) {
	selector := labels.Set{shipper.AppLabel: appName}.AsSelector()
	selectedRels, err := s.List(selector)
	if err != nil {
		return nil, err
	}

	type releaseAndGeneration struct {
		release    *shipper.Release
		generation int
	}

	filteredRels := make([]releaseAndGeneration, 0, len(selectedRels))
	for _, rel := range selectedRels {
		if rel.DeletionTimestamp != nil {
			continue
		}
		g, err := shippercontroller.GetReleaseGeneration(rel)
		if err != nil {
			return nil, fmt.Errorf(`incomplete Release "%s/%s": %s`, rel.Namespace, rel.Name, err)
		}
		filteredRels = append(filteredRels, releaseAndGeneration{rel, g})
	}

	sort.Slice(filteredRels, func(i, j int) bool {
		return filteredRels[i].generation < filteredRels[j].generation
	})

	relsToReturn := make([]*shipper.Release, 0, len(filteredRels))
	for _, e := range filteredRels {
		relsToReturn = append(relsToReturn, e.release)
	}

	return relsToReturn, nil
}

// ContenderForApplication returns the contender Release for the given application name.
func (s releaseNamespaceLister) ContenderForApplication(appName string) (*shipper.Release, error) {
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
func (s releaseNamespaceLister) ReleaseForInstallationTarget(it *shipper.InstallationTarget) (*shipper.Release, error) {
	owner, err := extractOwnerReference(it.ObjectMeta)
	if err != nil {
		return nil, err
	}

	rel, err := s.Get(owner.Name)
	if err != nil {
		return nil, err
	}

	if rel.UID != owner.UID {
		return nil, shippercontroller.NewWrongOwnerReferenceError(it.Name, it.UID, rel.UID)
	}

	return rel, nil
}

// extractOwnerReference returns an owner reference for the given object meta,
// or an error in the case there are multiple or no owner references.
func extractOwnerReference(it metav1.ObjectMeta) (*metav1.OwnerReference, error) {
	if n := len(it.OwnerReferences); n != 1 {
		return nil, shippercontroller.NewMultipleOwnerReferencesError(it.Name, n)
	}
	return &it.OwnerReferences[0], nil
}
