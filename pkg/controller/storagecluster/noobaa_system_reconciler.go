package storagecluster

import (
	"context"
	"reflect"

	"github.com/go-logr/logr"
	nbv1 "github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	objectreferencesv1 "github.com/openshift/custom-resource-status/objectreferences/v1"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
	statusutil "github.com/openshift/ocs-operator/pkg/controller/util"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/reference"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *ReconcileStorageCluster) ensureNoobaaSystem(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {

	nb := r.newNooBaaSystem(sc, reqLogger)

	err := controllerutil.SetControllerReference(sc, nb, r.scheme)
	if err != nil {
		return err
	}

	// check if this noobaa instance aleady exists
	found := &nbv1.NooBaa{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: nb.ObjectMeta.Name, Namespace: sc.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			// noobaa system not found - create one
			reqLogger.Info("Creating NooBaa system")
			err := r.client.Create(context.TODO(), nb)
			if err != nil {
				reqLogger.Error(err, "Failed to create NooBaa system")
				return err
			}
		} else {
			// other error. fail reconcile
			reqLogger.Error(err, "Failed to get NooBaa system")
			return err
		}
	} else {
		// Update NooBaa CR if it is not in the desired state
		if !reflect.DeepEqual(nb.Spec, found.Spec) {
			reqLogger.Info("Updating spec for NooBaa")
			found.Spec = nb.Spec
			err := r.client.Update(context.TODO(), found)
			if err != nil {
				reqLogger.Error(err, "Failed to update NooBaa system")
				return err
			}
		}

		objectRef, err := reference.GetReference(r.scheme, found)
		if err != nil {
			return err
		}
		objectreferencesv1.SetObjectReference(&sc.Status.RelatedObjects, *objectRef)
	}

	statusutil.MapNoobaaNegativeConditions(&r.conditions, found)

	return nil
}

func (r *ReconcileStorageCluster) newNooBaaSystem(sc *ocsv1.StorageCluster, reqLogger logr.Logger) *nbv1.NooBaa {
	storageClassName := generateNameForCephBlockPoolSC(sc)
	coreResources := defaults.GetDaemonResources("noobaa-core", sc.Spec.Resources)
	dbResources := defaults.GetDaemonResources("noobaa-db", sc.Spec.Resources)
	dBVolumeResources := defaults.GetDaemonResources("noobaa-db-vol", sc.Spec.Resources)
	nb := &nbv1.NooBaa{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "noobaa",
			Namespace: sc.Namespace,
			Labels: map[string]string{
				"app": "noobaa",
			},
		},

		Spec: nbv1.NooBaaSpec{
			DBStorageClass:            &storageClassName,
			PVPoolDefaultStorageClass: &storageClassName,
			CoreResources:             &coreResources,
			DBResources:               &dbResources,
			Tolerations:               defaults.DaemonPlacements["noobaa-core"].Tolerations,
			DBVolumeResources:         &dBVolumeResources,
		},
	}

	nb.Spec.Image = &r.noobaaCoreImage
	nb.Spec.DBImage = &r.noobaaDBImage

	return nb
}
