// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// CapacityTargetLister helps list CapacityTargets.
// All objects returned here must be treated as read-only.
type CapacityTargetLister interface {
	// List lists all CapacityTargets in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.CapacityTarget, err error)
	// CapacityTargets returns an object that can list and get CapacityTargets.
	CapacityTargets(namespace string) CapacityTargetNamespaceLister
	CapacityTargetListerExpansion
}

// capacityTargetLister implements the CapacityTargetLister interface.
type capacityTargetLister struct {
	indexer cache.Indexer
}

// NewCapacityTargetLister returns a new CapacityTargetLister.
func NewCapacityTargetLister(indexer cache.Indexer) CapacityTargetLister {
	return &capacityTargetLister{indexer: indexer}
}

// List lists all CapacityTargets in the indexer.
func (s *capacityTargetLister) List(selector labels.Selector) (ret []*v1alpha1.CapacityTarget, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.CapacityTarget))
	})
	return ret, err
}

// CapacityTargets returns an object that can list and get CapacityTargets.
func (s *capacityTargetLister) CapacityTargets(namespace string) CapacityTargetNamespaceLister {
	return capacityTargetNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// CapacityTargetNamespaceLister helps list and get CapacityTargets.
// All objects returned here must be treated as read-only.
type CapacityTargetNamespaceLister interface {
	// List lists all CapacityTargets in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.CapacityTarget, err error)
	// Get retrieves the CapacityTarget from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.CapacityTarget, error)
	CapacityTargetNamespaceListerExpansion
}

// capacityTargetNamespaceLister implements the CapacityTargetNamespaceLister
// interface.
type capacityTargetNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all CapacityTargets in the indexer for a given namespace.
func (s capacityTargetNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.CapacityTarget, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.CapacityTarget))
	})
	return ret, err
}

// Get retrieves the CapacityTarget from the indexer for a given namespace and name.
func (s capacityTargetNamespaceLister) Get(name string) (*v1alpha1.CapacityTarget, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("capacitytarget"), name)
	}
	return obj.(*v1alpha1.CapacityTarget), nil
}
