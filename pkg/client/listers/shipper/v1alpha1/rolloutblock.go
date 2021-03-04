// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// RolloutBlockLister helps list RolloutBlocks.
// All objects returned here must be treated as read-only.
type RolloutBlockLister interface {
	// List lists all RolloutBlocks in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.RolloutBlock, err error)
	// RolloutBlocks returns an object that can list and get RolloutBlocks.
	RolloutBlocks(namespace string) RolloutBlockNamespaceLister
	RolloutBlockListerExpansion
}

// rolloutBlockLister implements the RolloutBlockLister interface.
type rolloutBlockLister struct {
	indexer cache.Indexer
}

// NewRolloutBlockLister returns a new RolloutBlockLister.
func NewRolloutBlockLister(indexer cache.Indexer) RolloutBlockLister {
	return &rolloutBlockLister{indexer: indexer}
}

// List lists all RolloutBlocks in the indexer.
func (s *rolloutBlockLister) List(selector labels.Selector) (ret []*v1alpha1.RolloutBlock, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.RolloutBlock))
	})
	return ret, err
}

// RolloutBlocks returns an object that can list and get RolloutBlocks.
func (s *rolloutBlockLister) RolloutBlocks(namespace string) RolloutBlockNamespaceLister {
	return rolloutBlockNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// RolloutBlockNamespaceLister helps list and get RolloutBlocks.
// All objects returned here must be treated as read-only.
type RolloutBlockNamespaceLister interface {
	// List lists all RolloutBlocks in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.RolloutBlock, err error)
	// Get retrieves the RolloutBlock from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.RolloutBlock, error)
	RolloutBlockNamespaceListerExpansion
}

// rolloutBlockNamespaceLister implements the RolloutBlockNamespaceLister
// interface.
type rolloutBlockNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all RolloutBlocks in the indexer for a given namespace.
func (s rolloutBlockNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.RolloutBlock, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.RolloutBlock))
	})
	return ret, err
}

// Get retrieves the RolloutBlock from the indexer for a given namespace and name.
func (s rolloutBlockNamespaceLister) Get(name string) (*v1alpha1.RolloutBlock, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("rolloutblock"), name)
	}
	return obj.(*v1alpha1.RolloutBlock), nil
}
