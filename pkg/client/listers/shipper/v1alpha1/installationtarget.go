// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// InstallationTargetLister helps list InstallationTargets.
type InstallationTargetLister interface {
	// List lists all InstallationTargets in the indexer.
	List(selector labels.Selector) (ret []*v1alpha1.InstallationTarget, err error)
	// InstallationTargets returns an object that can list and get InstallationTargets.
	InstallationTargets(namespace string) InstallationTargetNamespaceLister
	InstallationTargetListerExpansion
}

// installationTargetLister implements the InstallationTargetLister interface.
type installationTargetLister struct {
	indexer cache.Indexer
}

// NewInstallationTargetLister returns a new InstallationTargetLister.
func NewInstallationTargetLister(indexer cache.Indexer) InstallationTargetLister {
	return &installationTargetLister{indexer: indexer}
}

// List lists all InstallationTargets in the indexer.
func (s *installationTargetLister) List(selector labels.Selector) (ret []*v1alpha1.InstallationTarget, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.InstallationTarget))
	})
	return ret, err
}

// InstallationTargets returns an object that can list and get InstallationTargets.
func (s *installationTargetLister) InstallationTargets(namespace string) InstallationTargetNamespaceLister {
	return installationTargetNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// InstallationTargetNamespaceLister helps list and get InstallationTargets.
type InstallationTargetNamespaceLister interface {
	// List lists all InstallationTargets in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*v1alpha1.InstallationTarget, err error)
	// Get retrieves the InstallationTarget from the indexer for a given namespace and name.
	Get(name string) (*v1alpha1.InstallationTarget, error)
	InstallationTargetNamespaceListerExpansion
}

// installationTargetNamespaceLister implements the InstallationTargetNamespaceLister
// interface.
type installationTargetNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all InstallationTargets in the indexer for a given namespace.
func (s installationTargetNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.InstallationTarget, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.InstallationTarget))
	})
	return ret, err
}

// Get retrieves the InstallationTarget from the indexer for a given namespace and name.
func (s installationTargetNamespaceLister) Get(name string) (*v1alpha1.InstallationTarget, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("installationtarget"), name)
	}
	return obj.(*v1alpha1.InstallationTarget), nil
}
