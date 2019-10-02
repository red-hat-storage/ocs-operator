package storageclusterinitialization

import (
	"context"
	"fmt"
	"os"
	"reflect"

	"github.com/go-logr/logr"
	nbv1 "github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *ReconcileStorageClusterInitialization) ensureNoobaaSystem(initialData *ocsv1.StorageClusterInitialization, reqLogger logr.Logger) error {

	nb := r.newNooBaaSystem(initialData, reqLogger)
	err := controllerutil.SetControllerReference(initialData, nb, r.scheme)
	if err != nil {
		return err
	}

	// check if this noobaa instance aleady exists
	found := &nbv1.NooBaa{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: nb.ObjectMeta.Name, Namespace: initialData.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating NooBaa system")
		err = r.client.Create(context.TODO(), nb)

	} else if err == nil {
		// Update NooBaa CR if it is not in the desired state
		if !reflect.DeepEqual(nb.Spec, found.Spec) {
			reqLogger.Info("Updating spec for NooBaa")
			found.Spec = nb.Spec
			err = r.client.Update(context.TODO(), found)
		}
	}

	return nil
}

func (r *ReconcileStorageClusterInitialization) newNooBaaSystem(initialData *ocsv1.StorageClusterInitialization, reqLogger logr.Logger) *nbv1.NooBaa {

	storageClassName := generateNameForCephBlockPoolSC(initialData)
	coreResources := defaults.GetDaemonResources("noobaa-core", initialData.Spec.Resources)
	dbResources := defaults.GetDaemonResources("noobaa-db", initialData.Spec.Resources)
	dBVolumeResources := defaults.GetDaemonResources("noobaa-db-vol", initialData.Spec.Resources)
	nb := &nbv1.NooBaa{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "noobaa",
			Namespace: initialData.Namespace,
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

	noobaaCoreImage := os.Getenv("NOOBAA_CORE_IMAGE")
	if noobaaCoreImage != "" {
		reqLogger.Info(fmt.Sprintf("setting noobaa system image to %s", noobaaCoreImage))
		nb.Spec.Image = &noobaaCoreImage
	}

	noobaaDBImage := os.Getenv("NOOBAA_DB_IMAGE")
	if noobaaDBImage != "" {
		reqLogger.Info(fmt.Sprintf("setting noobaa db image to %s", noobaaDBImage))
		nb.Spec.DBImage = &noobaaDBImage
	}
	return nb
}
