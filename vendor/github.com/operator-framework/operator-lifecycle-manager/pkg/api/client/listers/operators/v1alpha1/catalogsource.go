/*
Copyright Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	labels "k8s.io/apimachinery/pkg/labels"
	listers "k8s.io/client-go/listers"
	cache "k8s.io/client-go/tools/cache"
)

// CatalogSourceLister helps list CatalogSources.
// All objects returned here must be treated as read-only.
type CatalogSourceLister interface {
	// List lists all CatalogSources in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*operatorsv1alpha1.CatalogSource, err error)
	// CatalogSources returns an object that can list and get CatalogSources.
	CatalogSources(namespace string) CatalogSourceNamespaceLister
	CatalogSourceListerExpansion
}

// catalogSourceLister implements the CatalogSourceLister interface.
type catalogSourceLister struct {
	listers.ResourceIndexer[*operatorsv1alpha1.CatalogSource]
}

// NewCatalogSourceLister returns a new CatalogSourceLister.
func NewCatalogSourceLister(indexer cache.Indexer) CatalogSourceLister {
	return &catalogSourceLister{listers.New[*operatorsv1alpha1.CatalogSource](indexer, operatorsv1alpha1.Resource("catalogsource"))}
}

// CatalogSources returns an object that can list and get CatalogSources.
func (s *catalogSourceLister) CatalogSources(namespace string) CatalogSourceNamespaceLister {
	return catalogSourceNamespaceLister{listers.NewNamespaced[*operatorsv1alpha1.CatalogSource](s.ResourceIndexer, namespace)}
}

// CatalogSourceNamespaceLister helps list and get CatalogSources.
// All objects returned here must be treated as read-only.
type CatalogSourceNamespaceLister interface {
	// List lists all CatalogSources in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*operatorsv1alpha1.CatalogSource, err error)
	// Get retrieves the CatalogSource from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*operatorsv1alpha1.CatalogSource, error)
	CatalogSourceNamespaceListerExpansion
}

// catalogSourceNamespaceLister implements the CatalogSourceNamespaceLister
// interface.
type catalogSourceNamespaceLister struct {
	listers.ResourceIndexer[*operatorsv1alpha1.CatalogSource]
}
