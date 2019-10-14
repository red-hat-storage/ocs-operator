package v1

import (
	v1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"k8s.io/client-go/rest"
)

// StorageClusterGetter has a method to return a StorageClusterInterface.
// A group's client should implement this interface.
type StorageClusterGetter interface {
	StorageClusters(namespace string) StorageClusterInterface
}

// StorageClusterInterface has methods to work with StorageCluster resources.
type StorageClusterInterface interface {
	Create(*v1.StorageCluster) (*v1.StorageCluster, error)
	// Update(*v1.StorageCluster) (*v1.StorageCluster, error)
	// Delete(name string, options *metav1.DeleteOptions) error
	// DeleteCollection(options *metav1.DeleteOptions, listOptions metav1.ListOptions) error
	// Get(name string, options metav1.GetOptions) (*v1.StorageCluster, error)
	// List(opts metav1.ListOptions) (*v1.StorageClusterList, error)
	// Watch(opts metav1.ListOptions) (watch.Interface, error)
	// Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1.StorageCluster, err error)
	StorageClusterExpansion
}

// storageclusters implements StorageClusterInterface
type storageclusters struct {
	client rest.Interface
	ns     string
}

// newStorageClusters returns a storageclusters
func newStorageClusters(c *OcsV1Client, namespace string) *storageclusters {
	return &storageclusters{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Create takes the representation of a storagecluster and creates it.  Returns the server's representation of the storagecluster, and an error, if there is any.
func (c *storageclusters) Create(storagecluster *v1.StorageCluster) (result *v1.StorageCluster, err error) {
	result = &v1.StorageCluster{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("storageclusters").
		Body(storagecluster).
		Do().
		Into(result)
	return
}
