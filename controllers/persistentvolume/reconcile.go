package persistentvolume

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ensureFunc which encapsulate all the 'ensure*' type functions
type ensureFunc func(*corev1.PersistentVolume) error

// +kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch;create;update;delete

// Reconcile ...
func (r *PersistentVolumeReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {

	prevLogger := r.Log
	defer func() { r.Log = prevLogger }()
	r.Log = r.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)

	pv := &corev1.PersistentVolume{}
	err := r.Client.Get(ctx, request.NamespacedName, pv)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("PersistentVolume not found.", "PersistentVolume", request.Name)
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Check GetDeletionTimestamp to determine if the object is under deletion
	if !pv.GetDeletionTimestamp().IsZero() {
		r.Log.Info("PersistentVolume is terminated, skipping reconciliation.", "PersistentVolume", pv.Name)
		return reconcile.Result{}, nil
	}

	r.Log.Info("Reconciling PersistentVolume.", "PersistentVolume", pv.Name)

	ensureFs := []ensureFunc{
		r.ensureExpansionSecret,
	}
	for _, f := range ensureFs {
		err = f(pv)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

func (r *PersistentVolumeReconciler) ensureExpansionSecret(pv *corev1.PersistentVolume) error {
	scName := pv.Spec.StorageClassName
	if scName == "" {
		r.Log.Info("PersistentVolume has no associated StorageClass.", "PersistentVolume", pv.Name)
		return nil
	}
	sc := &storagev1.StorageClass{}

	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: scName}, sc)
	if err != nil {
		r.Log.Error(err, "Error getting StorageClass.", "StorageClass", scName)
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
		r.Log.Error(err, "Error patching PersistentVolume.", "PersistentVolume", newPV.Name)
		return err
	}

	r.Log.Info("PersistentVolume patched.", "PersistentVolume", newPV.Name)
	return nil
}
