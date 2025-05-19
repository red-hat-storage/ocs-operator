package storagecluster

import (
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/platform"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	storagev1 "k8s.io/api/storage/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type obcStorageClasses struct{}

var _ resourceManager = &obcStorageClasses{}

func (s *obcStorageClasses) ensureCreated(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (ctrl.Result, error) {

	if skip, err := platform.PlatformsShouldSkipObjectStore(); err != nil {
		r.Log.Error(err, "failed to identify if ObjectStore SC should be created")
	} else if skip {
		return ctrl.Result{}, nil
	}

	sc := util.NewDefaultOBCStorageClass(
		storageCluster.Namespace,
		util.GenerateNameForCephObjectStore(storageCluster),
	)

	storageClass := &storagev1.StorageClass{}
	storageClass.Name = util.GenerateNameForCephRgwStorageClass(storageCluster)

	if err := util.CreateOrReplace(r.ctx, r.Client, storageClass, func() error {
		storageClass.Parameters = sc.Parameters
		storageClass.Provisioner = sc.Provisioner
		storageClass.ReclaimPolicy = sc.ReclaimPolicy
		storageClass.Annotations = sc.Annotations
		return nil
	}); err != nil {
		r.Log.Error(err, "unable to create or update the OBC StorageClass")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (s *obcStorageClasses) ensureDeleted(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (ctrl.Result, error) {
	scName := util.GenerateNameForCephRgwStorageClass(storageCluster)

	storageClass := &storagev1.StorageClass{}
	storageClass.Name = scName

	if err := r.Client.Delete(r.ctx, storageClass); client.IgnoreNotFound(err) != nil {
		r.Log.Error(err, "failed to delete OBC StorageClass", "Name", scName)
		return ctrl.Result{}, err
	}
	r.Log.Info("Deleted OBC StorageClass", "Name", scName)
	return ctrl.Result{}, nil
}
