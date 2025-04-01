package storagecluster

import (
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/platform"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

	sc.Name = util.GenerateNameForCephRgwSC(storageCluster)
	sc.Namespace = storageCluster.Namespace

	if _, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, sc, func() error {
		return nil
	}); err != nil {
		r.Log.Error(err, "unable to create or update the OBC StorageClass")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (s *obcStorageClasses) ensureDeleted(_ *StorageClusterReconciler, _ *ocsv1.StorageCluster) (ctrl.Result, error) {
	// cleaned up via owner references
	return ctrl.Result{}, nil
}
