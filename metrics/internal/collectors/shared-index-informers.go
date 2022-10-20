package collectors

import (
	"github.com/red-hat-storage/ocs-operator/metrics/internal/options"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type RESTClientInterface interface {
	RESTClient() restclient.Interface
}

type SharedIndexInformerArrayIndex int

const (
	CephClusterSIIAI SharedIndexInformerArrayIndex = iota
	CephObjectStoreSIIAI
	StorageClassSIIAI
	CephRBDMirrorSIIAI
	CephBlockPoolSIIAI
)

func (si SharedIndexInformerArrayIndex) Resource() string {
	resourceArr := []string{
		"cephclusters",
		"cephobjectstores",
		"storageclasses",
		"cephrbdmirrors",
		"cephblockpools",
	}
	return resourceArr[si]
}

func (si SharedIndexInformerArrayIndex) RuntimeObject() runtime.Object {
	runtimeObjectArr := []runtime.Object{
		&cephv1.CephCluster{},
		&cephv1.CephObjectStore{},
		&storagev1.StorageClass{},
		&cephv1.CephRBDMirror{},
		&cephv1.CephBlockPool{},
	}
	return runtimeObjectArr[si]
}

func (si SharedIndexInformerArrayIndex) SharedIndexInformer(restClient RESTClientInterface) cache.SharedIndexInformer {
	return getSharedIndexInformer(restClient, si.Resource(), si.RuntimeObject())
}

func getSharedIndexInformer(restClient RESTClientInterface, resource string, sampleObject runtime.Object) cache.SharedIndexInformer {
	lw := cache.NewListWatchFromClient(restClient.RESTClient(), resource, metav1.NamespaceAll, fields.Everything())
	return cache.NewSharedIndexInformer(lw, sampleObject, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

func listAllSharedIndexInformers(opts *options.Options) (siiArr []cache.SharedIndexInformer) {
	k8sClient := clientset.NewForConfigOrDie(opts.Kubeconfig)
	rookClient := rookclient.NewForConfigOrDie(opts.Kubeconfig)
	siiArr = []cache.SharedIndexInformer{
		CephClusterSIIAI.SharedIndexInformer(rookClient.CephV1()),
		CephObjectStoreSIIAI.SharedIndexInformer(rookClient.CephV1()),
		StorageClassSIIAI.SharedIndexInformer(k8sClient),
		CephRBDMirrorSIIAI.SharedIndexInformer(rookClient.CephV1()),
		CephBlockPoolSIIAI.SharedIndexInformer(rookClient.CephV1()),
	}
	return
}
