package v1

import (
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shippercontroller "github.com/bookingcom/shipper/pkg/controller"
	"github.com/bookingcom/shipper/pkg/errors"
	"github.com/bookingcom/shipper/pkg/util/release"
)

// ReleaseListerExpansion allows custom methods to be added to
// ReleaseLister.
type ReleaseListerExpansion interface{}

// ReleaseNamespaceListerExpansion allows custom methods to be added to
// ReleaseNamespaceLister.
type ReleaseNamespaceListerExpansion interface {
	ReleasesForApplication(appName string) ([]*shipperv1.Release, error)
	ContenderForApplication(appName string) (*shipperv1.Release, error)
	IncumbentForApplication(appName string) (*shipperv1.Release, error)
	ReleaseForInstallationTarget(it *shipperv1.InstallationTarget) (*shipperv1.Release, error)
	TransitionPairForApplication(appName string) (*shipperv1.Release, *shipperv1.Release, error)
}

// ReleasesForApplication returns Releases related to the given application
// name ordered by generation.
func (s releaseNamespaceLister) ReleasesForApplication(appName string) ([]*shipperv1.Release, error) {
	selector := labels.Set{shipperv1.AppLabel: appName}.AsSelector()
	selectedRels, err := s.List(selector)
	if err != nil {
		return nil, err
	}

	type releaseAndGeneration struct {
		release    *shipperv1.Release
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

	relsToReturn := make([]*shipperv1.Release, 0, len(filteredRels))
	for _, e := range filteredRels {
		relsToReturn = append(relsToReturn, e.release)
	}

	return relsToReturn, nil
}

// ContenderForApplication returns the contender Release for the given application name.
func (s releaseNamespaceLister) ContenderForApplication(appName string) (*shipperv1.Release, error) {
	rels, err := s.ReleasesForApplication(appName)
	if err != nil {
		return nil, err
	}
	if len(rels) == 0 {
		return nil, errors.NewContenderNotFoundError(appName)
	}
	return rels[len(rels)-1], nil
}

// IncumbentForApplication returns the incumbent Release for the given application name.
func (s releaseNamespaceLister) IncumbentForApplication(appName string) (*shipperv1.Release, error) {
	rels, err := s.ReleasesForApplication(appName)
	if err != nil {
		return nil, err
	}
	for _, r := range rels {
		if release.ReleaseComplete(r) {
			return r, nil
		}
	}
	return nil, errors.NewIncumbentNotFoundError(appName)
}

func (s releaseNamespaceLister) TransitionPairForApplication(appName string) (*shipperv1.Release, *shipperV1.Release,
	error) {
	contenderRel, err := s.ContenderForApplication(appName)
	if err != nil {
		return nil, nil, err
	}

	incumbentRel, err := s.IncumbentForApplication(appName)
	if err != nil {
		return nil, nil, err
	}

	return contenderRel, incumbentRel, nil
}

// ReleaseForInstallationTarget returns the Release associated with given
// InstallationTarget. The relationship is established through owner
// references.
func (s releaseNamespaceLister) ReleaseForInstallationTarget(it *shipperv1.InstallationTarget) (*shipperv1.Release, error) {
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
