package persistentvolume

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ensureFunc which encapsulate all the 'ensure*' type functions
type ensureFunc func(*corev1.PersistentVolume, logr.Logger) error

// Reconcile ...
func (r *PersistentVolumeReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := r.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)

	pv := &corev1.PersistentVolume{}
	err := r.Client.Get(context.TODO(), request.NamespacedName, pv)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("PersistentVolume not found")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Check GetDeletionTimestamp to determine if the object is under deletion
	if !pv.GetDeletionTimestamp().IsZero() {
		reqLogger.Info("Object is terminated, skipping reconciliation")
		return reconcile.Result{}, nil
	}

	reqLogger.Info("Reconciling PersistentVolume")

	ensureFs := []ensureFunc{
		r.ensureExpansionSecret,
	}
	for _, f := range ensureFs {
		err = f(pv, reqLogger)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

func (r *PersistentVolumeReconciler) ensureExpansionSecret(pv *corev1.PersistentVolume, reqLogger logr.Logger) error {
	scName := pv.Spec.StorageClassName
	if scName == "" {
		reqLogger.Info("PersistentVolume has no associated StorageClass")
		return nil
	}
	sc := &storagev1.StorageClass{}

	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: scName}, sc)
	if err != nil {
		reqLogger.Error(err, "Error getting StorageClass")
		return err
	}

	patch := client.MergeFrom(pv)

	secretRef := &corev1.SecretReference{}

	if secretName, ok := sc.Parameters[csiExpansionSecretName]; ok {
		secretRef.Name = secretName
	}
	if secretNamespace, ok := sc.Parameters[csiExpansionSecretNamespace]; ok {
		secretRef.Namespace = secretNamespace
	}

	newPV := &corev1.PersistentVolume{}
	pv.DeepCopyInto(newPV)

	newPV.Spec.CSI.ControllerExpandSecretRef = secretRef
	err = r.Client.Patch(context.TODO(), newPV, patch)
	if err != nil {
		reqLogger.Error(err, "Error patching PersistentVolume")
		return err
	}

	reqLogger.Info("PersistentVolume patched")
	return nil
}
