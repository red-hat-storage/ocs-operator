package collectors

import (
	"testing"

	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestSharedIndexInformers(t *testing.T) {
	resourceStrArr := []string{
		"cephclusters",
		"cephblockpools",
		"cephrbdmirrors",
		"cephobjectstores",
		"storageclasses",
	}

	runtimeObjs := []runtime.Object{
		&cephv1.CephCluster{},
		&cephv1.CephBlockPool{},
		&cephv1.CephRBDMirror{},
		&cephv1.CephObjectStore{},
		&storagev1.StorageClass{},
	}

	sharedIndexInformerAIArr := []SharedIndexInformerArrayIndex{
		CephClusterSIIAI,
		CephBlockPoolSIIAI,
		CephRBDMirrorSIIAI,
		CephObjectStoreSIIAI,
		StorageClassSIIAI,
	}

	for idx, sharedIndexInformerAI := range sharedIndexInformerAIArr {
		assert.Equal(t, resourceStrArr[idx], sharedIndexInformerAI.Resource())
		assert.Equal(t, runtimeObjs[idx], sharedIndexInformerAI.RuntimeObject())
	}

	setKubeConfig(t)
	siiList := listAllSharedIndexInformers(mockOpts)
	assert.Equal(t, len(sharedIndexInformerAIArr), len(siiList))
}
