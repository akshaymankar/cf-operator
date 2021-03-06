/*

Don't alter this file, it was generated.

*/
// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "code.cloudfoundry.org/cf-operator/pkg/kube/apis/extendedsecret/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// ExtendedSecretLister helps list ExtendedSecrets.
type ExtendedSecretLister interface {
	// List lists all ExtendedSecrets in the indexer.
	List(selector labels.Selector) (ret []*v1alpha1.ExtendedSecret, err error)
	// ExtendedSecrets returns an object that can list and get ExtendedSecrets.
	ExtendedSecrets(namespace string) ExtendedSecretNamespaceLister
	ExtendedSecretListerExpansion
}

// extendedSecretLister implements the ExtendedSecretLister interface.
type extendedSecretLister struct {
	indexer cache.Indexer
}

// NewExtendedSecretLister returns a new ExtendedSecretLister.
func NewExtendedSecretLister(indexer cache.Indexer) ExtendedSecretLister {
	return &extendedSecretLister{indexer: indexer}
}

// List lists all ExtendedSecrets in the indexer.
func (s *extendedSecretLister) List(selector labels.Selector) (ret []*v1alpha1.ExtendedSecret, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.ExtendedSecret))
	})
	return ret, err
}

// ExtendedSecrets returns an object that can list and get ExtendedSecrets.
func (s *extendedSecretLister) ExtendedSecrets(namespace string) ExtendedSecretNamespaceLister {
	return extendedSecretNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// ExtendedSecretNamespaceLister helps list and get ExtendedSecrets.
type ExtendedSecretNamespaceLister interface {
	// List lists all ExtendedSecrets in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*v1alpha1.ExtendedSecret, err error)
	// Get retrieves the ExtendedSecret from the indexer for a given namespace and name.
	Get(name string) (*v1alpha1.ExtendedSecret, error)
	ExtendedSecretNamespaceListerExpansion
}

// extendedSecretNamespaceLister implements the ExtendedSecretNamespaceLister
// interface.
type extendedSecretNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all ExtendedSecrets in the indexer for a given namespace.
func (s extendedSecretNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.ExtendedSecret, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.ExtendedSecret))
	})
	return ret, err
}

// Get retrieves the ExtendedSecret from the indexer for a given namespace and name.
func (s extendedSecretNamespaceLister) Get(name string) (*v1alpha1.ExtendedSecret, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("extendedsecret"), name)
	}
	return obj.(*v1alpha1.ExtendedSecret), nil
}
