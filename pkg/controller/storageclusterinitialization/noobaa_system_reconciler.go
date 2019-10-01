package storageclusterinitialization

import (
	"context"
	"fmt"
	"os"
	"reflect"

	"github.com/go-logr/logr"
	nbv1 "github.com/noobaa/noobaa-operator/pkg/apis/noobaa/v1alpha1"
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
	coreResources := defaults.GetDaemonResources("noobaa", initialData.Spec.Resources)
	nb := &nbv1.NooBaa{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "noobaa",
			Namespace: initialData.Namespace,
			Labels: map[string]string{
				"app": "noobaa",
			},
		},

		Spec: nbv1.NooBaaSpec{
			StorageClassName: &storageClassName,
			CoreResources:    &coreResources,
		},
	}

	noobaaCoreImage := os.Getenv("NOOBAA_CORE_IMAGE")
	if noobaaCoreImage != "" {
		reqLogger.Info(fmt.Sprintf("setting noobaa system image to %s", noobaaCoreImage))
		nb.Spec.Image = &noobaaCoreImage
	}

	noobaaMongoImage := os.Getenv("NOOBAA_MONGODB_IMAGE")
	if noobaaMongoImage != "" {
		reqLogger.Info(fmt.Sprintf("setting noobaa mongodb image to %s", noobaaMongoImage))
		nb.Spec.MongoImage = &noobaaMongoImage
	}
	return nb
}
