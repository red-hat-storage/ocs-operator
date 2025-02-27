package storagecluster

import (
	"context"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/platform"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ocsCephObjectStores struct{}

// ensureCreated ensures that CephObjectStore resources exist in the desired
// state.
func (obj *ocsCephObjectStores) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	reconcileStrategy := ReconcileStrategy(instance.Spec.ManagedResources.CephObjectStores.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return reconcile.Result{}, nil
	}

	skip, err := platform.PlatformsShouldSkipObjectStore()
	if err != nil {
		return reconcile.Result{}, err
	}

	if skip {
		platformType, err := platform.GetPlatformType()
		if err != nil {
			return reconcile.Result{}, err
		}
		r.Log.Info("Platform is set to skip object store. Not creating a CephObjectStore.", "Platform", platformType)
		return reconcile.Result{}, nil
	}
	var cephObjectStores []*cephv1.CephObjectStore
	// Add KMS details to cephObjectStores spec, only if
	// cluster-wide encryption is enabled or any of the device set is encrypted
	// ie, sc.Spec.Encryption.ClusterWide/sc.Spec.Encryption.Enable is True or any of the deviceSet is encrypted
	// and KMS ConfigMap is available
	if util.IsClusterOrDeviceSetEncrypted(instance) {
		kmsConfigMap, err := getKMSConfigMap(KMSConfigMapName, instance, r.Client)
		if err != nil {
			r.Log.Error(err, "Failed to procure KMS ConfigMap.", "KMSConfigMap", klog.KRef(instance.Namespace, KMSConfigMapName))
			return reconcile.Result{}, err
		}
		if kmsConfigMap != nil {
			if err = reachKMSProvider(kmsConfigMap); err != nil {
				r.Log.Error(err, "Address provided in KMS ConfigMap is not reachable.", "KMSConfigMap", klog.KRef(kmsConfigMap.Namespace, kmsConfigMap.Name))
				return reconcile.Result{}, err
			}
		}
		cephObjectStores, err = r.newCephObjectStoreInstances(instance, kmsConfigMap)
		if err != nil {
			return reconcile.Result{}, err
		}
	} else {
		var err error
		cephObjectStores, err = r.newCephObjectStoreInstances(instance, nil)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	err = r.createCephObjectStores(cephObjectStores, instance)
	if err != nil {
		r.Log.Error(err, "Unable to create CephObjectStores for StorageCluster.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// ensureDeleted deletes the CephObjectStores owned by the StorageCluster
func (obj *ocsCephObjectStores) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	foundCephObjectStore := &cephv1.CephObjectStore{}
	cephObjectStores, err := r.newCephObjectStoreInstances(sc, nil)
	if err != nil {
		return reconcile.Result{}, err
	}

	for _, cephObjectStore := range cephObjectStores {
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStore.Name, Namespace: sc.Namespace}, foundCephObjectStore)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: CephObjectStore not found.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
				continue
			}
			return reconcile.Result{}, fmt.Errorf("Uninstall: Unable to retrieve CephObjectStore %v: %v", cephObjectStore.Name, err)
		}

		if cephObjectStore.GetDeletionTimestamp().IsZero() {
			r.Log.Info("Uninstall: Deleting CephObjectStore.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
			err = r.Client.Delete(context.TODO(), foundCephObjectStore)
			if err != nil {
				r.Log.Error(err, "Uninstall: Failed to delete CephObjectStore.", klog.KRef(foundCephObjectStore.Namespace, foundCephObjectStore.Name))
				return reconcile.Result{}, fmt.Errorf("uninstall: Failed to delete CephObjectStore %v: %v", foundCephObjectStore.Name, err)
			}
		}

		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStore.Name, Namespace: sc.Namespace}, foundCephObjectStore)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: CephObjectStore is deleted.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
				continue
			}
		}
		r.Log.Error(err, "Uninstall: Waiting for CephObjectStore to be deleted.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
		return reconcile.Result{}, fmt.Errorf("uninstall: Waiting for CephObjectStore %v to be deleted", cephObjectStore.Name)

	}
	return reconcile.Result{}, nil
}

// createCephObjectStore creates CephObjectStore in the desired state
func (r *StorageClusterReconciler) createCephObjectStores(cephObjectStores []*cephv1.CephObjectStore, instance *ocsv1.StorageCluster) error {
	for _, cephObjectStore := range cephObjectStores {
		existing := cephv1.CephObjectStore{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStore.Name, Namespace: cephObjectStore.Namespace}, &existing)
		switch {
		case err == nil:
			reconcileStrategy := ReconcileStrategy(instance.Spec.ManagedResources.CephObjectStores.ReconcileStrategy)
			if reconcileStrategy == ReconcileStrategyInit {
				return nil
			}
			if existing.DeletionTimestamp != nil {
				err := fmt.Errorf("failed to restore CephObjectStore object %s because it is marked for deletion", existing.Name)
				r.Log.Info("Failed to restore CephObjectStore because it is marked for deletion.", "CephObjectStore", klog.KRef(existing.Namespace, existing.Name))
				return err
			}

			r.Log.Info("Restoring original CephObjectStore.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
			existing.ObjectMeta.OwnerReferences = cephObjectStore.ObjectMeta.OwnerReferences
			cephObjectStore.ObjectMeta = existing.ObjectMeta
			err = r.Client.Update(context.TODO(), cephObjectStore)
			if err != nil {
				r.Log.Error(err, "Failed to update CephObjectStore.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
				return err
			}
		case errors.IsNotFound(err):
			r.Log.Info("Creating CephObjectStore.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
			err = r.Client.Create(context.TODO(), cephObjectStore)
			if err != nil {
				r.Log.Error(err, "Failed to create CephObjectStore.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
				return err
			}
		}
	}
	return nil
}

// newCephObjectStoreInstances returns the cephObjectStore instances that should be created
// on first run.
func (r *StorageClusterReconciler) newCephObjectStoreInstances(initData *ocsv1.StorageCluster, kmsConfigMap *corev1.ConfigMap) ([]*cephv1.CephObjectStore, error) {
	ret := []*cephv1.CephObjectStore{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForCephObjectStore(initData),
				Namespace: initData.Namespace,
			},
			Spec: cephv1.ObjectStoreSpec{
				PreservePoolsOnDelete: false,
				DataPool:              initData.Spec.ManagedResources.CephObjectStores.DataPoolSpec,     // Pass the poolSpec from the storageCluster CR
				MetadataPool:          initData.Spec.ManagedResources.CephObjectStores.MetadataPoolSpec, // Pass the poolSpec from the storageCluster CR
				Gateway: cephv1.GatewaySpec{
					Port:       80,
					SecurePort: 443,
					Service: &cephv1.RGWServiceSpec{
						Annotations: map[string]string{
							"service.beta.openshift.io/serving-cert-secret-name": fmt.Sprintf("%s-%s-%s", initData.Name, "cos", cephRgwTLSSecretKey),
						},
					},
					Instances: int32(getCephObjectStoreGatewayInstances(initData)),
					Placement: getPlacement(initData, "rgw"),
					Resources: defaults.GetProfileDaemonResources("rgw", initData),
					// set PriorityClassName for the rgw pods
					PriorityClassName: openshiftUserCritical,
					Labels:            cephv1.Labels{defaults.ODFResourceProfileKey: initData.Spec.ResourceProfile},
				},
			},
		},
	}
	for _, obj := range ret {
		err := controllerutil.SetControllerReference(initData, obj, r.Scheme)
		if err != nil {
			r.Log.Error(err, "Failed to set ControllerReference for CephObjectStore.", "CephObjectStore", klog.KRef(obj.Namespace, obj.Name))
			return nil, err
		}

		if initData.Spec.ManagedResources.CephObjectStores.HostNetwork != nil {
			obj.Spec.Gateway.HostNetwork = initData.Spec.ManagedResources.CephObjectStores.HostNetwork
		}

		// Set default values in the data pool spec as necessary
		setDefaultDataPoolSpec(&obj.Spec.DataPool, initData)

		// Set default values in the metadata pool spec as necessary
		setDefaultMetadataPoolSpec(&obj.Spec.MetadataPool, initData)

		// if kmsConfig is not 'nil', add the KMS details to ObjectStore spec
		if kmsConfigMap != nil {

			// skip if KMS_PROVIDER is ibmkeyprotect or VAULT_AUTH_METHOD is kubernetes, not supported for RGW
			if kmsConfigMap.Data["KMS_PROVIDER"] == IbmKeyProtectKMSProvider ||
				kmsConfigMap.Data["KMS_PROVIDER"] == ThalesKMSProvider ||
				kmsConfigMap.Data["VAULT_AUTH_METHOD"] == VaultSAAuthMethod {
				r.Log.Info("IBMKeyProtect/Thales as KMS provider or Vault authentication via Service Account is unsupported configuration for RGW KMS, hence skipping")
				continue
			}
			// Set default KMS_PROVIDER and VAULT_SECRET_ENGINE values, refer https://issues.redhat.com/browse/RHSTOR-1963
			if _, ok := kmsConfigMap.Data["KMS_PROVIDER"]; !ok {
				kmsConfigMap.Data["KMS_PROVIDER"] = VaultKMSProvider
			}
			rgwConnDetails := make(map[string]string)
			for key, value := range kmsConfigMap.Data {
				// ignore kv engine related options
				if key == "VAULT_BACKEND_PATH" || key == "VAULT_BACKEND" {
					continue
				}
				rgwConnDetails[key] = value
			}
			// overwrite SecretEngine value to transit
			rgwConnDetails["VAULT_SECRET_ENGINE"] = "transit"
			obj.Spec.Security = &cephv1.ObjectStoreSecuritySpec{
				SecuritySpec: cephv1.SecuritySpec{
					KeyManagementService: cephv1.KeyManagementServiceSpec{
						ConnectionDetails: rgwConnDetails,
						TokenSecretName:   KMSTokenSecretName,
					},
				},
			}
		}
	}
	return ret, nil
}

func getCephObjectStoreGatewayInstances(sc *ocsv1.StorageCluster) int {
	if arbiterEnabled(sc) {
		return defaults.ArbiterCephObjectStoreGatewayInstances
	}
	customGatewayInstances := sc.Spec.ManagedResources.CephObjectStores.GatewayInstances
	if customGatewayInstances != 0 {
		return customGatewayInstances
	}
	return defaults.CephObjectStoreGatewayInstances
}
