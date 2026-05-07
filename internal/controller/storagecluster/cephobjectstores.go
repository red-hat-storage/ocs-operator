package storagecluster

import (
	"context"
	"crypto/rand"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/platform"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"
	ocstlsv1 "github.com/red-hat-storage/ocs-tls-profiles/api/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	enableRGWAutoscaleAnnotation = "ocs.openshift.io/enable-rgw-autoscale"
	stsKeyLen                    = 16 // 16 alphanumeric characters for STS key
)

type ocsCephObjectStores struct {
	tlsProfile *ocstlsv1.TLSProfile
}

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
		kmsConfigMap, err := util.GetKMSConfigMap(defaults.KMSConfigMapName, instance, r.Client)
		if err != nil {
			r.Log.Error(err, "Failed to procure KMS ConfigMap.", "KMSConfigMap", klog.KRef(instance.Namespace, defaults.KMSConfigMapName))
			return reconcile.Result{}, err
		}
		if kmsConfigMap != nil {
			if err = reachKMSProvider(kmsConfigMap); err != nil {
				r.Log.Error(err, "Address provided in KMS ConfigMap is not reachable.", "KMSConfigMap", klog.KRef(kmsConfigMap.Namespace, kmsConfigMap.Name))
				return reconcile.Result{}, err
			}
		}
		cephObjectStores, err = r.newCephObjectStoreInstances(instance, kmsConfigMap, obj.tlsProfile)
		if err != nil {
			return reconcile.Result{}, err
		}
	} else {
		var err error
		cephObjectStores, err = r.newCephObjectStoreInstances(instance, nil, obj.tlsProfile)
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
	cephObjectStores, err := r.newCephObjectStoreInstances(sc, nil, obj.tlsProfile)
	if err != nil {
		return reconcile.Result{}, err
	}

	for _, cephObjectStore := range cephObjectStores {
		err := r.Get(context.TODO(), types.NamespacedName{Name: cephObjectStore.Name, Namespace: sc.Namespace}, foundCephObjectStore)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: CephObjectStore not found.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
				continue
			}
			return reconcile.Result{}, fmt.Errorf("uninstall: unable to retrieve CephObjectStore %v: %v", cephObjectStore.Name, err)
		}

		if cephObjectStore.GetDeletionTimestamp().IsZero() {
			r.Log.Info("Uninstall: Deleting CephObjectStore.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
			err = r.Delete(context.TODO(), foundCephObjectStore)
			if err != nil {
				r.Log.Error(err, "Uninstall: Failed to delete CephObjectStore.", "name", klog.KRef(foundCephObjectStore.Namespace, foundCephObjectStore.Name))
				return reconcile.Result{}, fmt.Errorf("uninstall: Failed to delete CephObjectStore %v: %v", foundCephObjectStore.Name, err)
			}
		}

		err = r.Get(context.TODO(), types.NamespacedName{Name: cephObjectStore.Name, Namespace: sc.Namespace}, foundCephObjectStore)
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
		err := r.Get(context.TODO(), types.NamespacedName{Name: cephObjectStore.Name, Namespace: cephObjectStore.Namespace}, &existing)
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
			existing.OwnerReferences = cephObjectStore.OwnerReferences
			cephObjectStore.ObjectMeta = existing.ObjectMeta

			// preserving any existing rgw ReadAffinities
			if existing.Spec.Gateway.ReadAffinity != nil {
				cephObjectStore.Spec.Gateway.ReadAffinity = existing.Spec.Gateway.ReadAffinity
			}

			// Skip overriding the RGW instances if `ocs.openshift.io/enable-RGW-autoscale:true` annotation is set on the StorageCluster.
			// This annotation will be added if the user wants HPA(KEDA) to autoscale the RGW instances.
			if annotation, found := instance.GetAnnotations()[enableRGWAutoscaleAnnotation]; found && annotation == "true" {
				r.Log.Info("skip overrridng the RGW instances count in the existing CephObjectStore via StorageCluster CR since HPA is enabled", "CephObjectStore", klog.KRef(existing.Namespace, existing.Name))
				cephObjectStore.Spec.Gateway.Instances = existing.Spec.Gateway.Instances
			}

			err = r.Update(context.TODO(), cephObjectStore)
			if err != nil {
				r.Log.Error(err, "Failed to update CephObjectStore.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
				return err
			}
		case errors.IsNotFound(err):
			r.Log.Info("Creating CephObjectStore.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
			err = r.Create(context.TODO(), cephObjectStore)
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
func (r *StorageClusterReconciler) newCephObjectStoreInstances(initData *ocsv1.StorageCluster, kmsConfigMap *corev1.ConfigMap, tlsProfile *ocstlsv1.TLSProfile) ([]*cephv1.CephObjectStore, error) {
	ret := []*cephv1.CephObjectStore{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      util.GenerateNameForCephObjectStore(initData),
				Namespace: initData.Namespace,
			},
			Spec: cephv1.ObjectStoreSpec{
				PreservePoolsOnDelete: false,
				DataPool: func() cephv1.PoolSpec {
					if initData.Spec.ManagedResources.CephObjectStores.DataPoolSpec != nil {
						return *initData.Spec.ManagedResources.CephObjectStores.DataPoolSpec
					}
					return cephv1.PoolSpec{}
				}(),
				MetadataPool: func() cephv1.PoolSpec {
					if initData.Spec.ManagedResources.CephObjectStores.MetadataPoolSpec != nil {
						return *initData.Spec.ManagedResources.CephObjectStores.MetadataPoolSpec
					}
					return cephv1.PoolSpec{}
				}(),
				Gateway: cephv1.GatewaySpec{
					Port:       getRGWPort(initData),
					SecurePort: getRGWSecurePort(initData),
					Service: &cephv1.RGWServiceSpec{
						Annotations: map[string]string{
							"service.beta.openshift.io/serving-cert-secret-name": fmt.Sprintf("%s-%s-%s", initData.Name, "cos", cephRgwTLSSecretKey),
						},
					},
					Instances: int32(getCephObjectStoreGatewayInstances(initData)),
					Placement: GetPlacement(initData, "rgw"),
					Resources: getDaemonResources("rgw", initData),
					// set PriorityClassName for the rgw pods
					PriorityClassName: systemClusterCritical,
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

		// when using non default hostNetwork Object Store should use hostNetwork
		obj.Spec.Gateway.HostNetwork = ptr.To(util.ShouldUseHostNetworking(initData) || ptr.Deref(initData.Spec.ManagedResources.CephObjectStores.HostNetwork, false))

		// Set default values in the data pool spec as necessary
		setDefaultDataPoolSpec(&obj.Spec.DataPool, initData)

		// Set default values in the metadata pool spec as necessary
		setDefaultMetadataPoolSpec(&obj.Spec.MetadataPool, initData)

		// if kmsConfig is not 'nil', add encryption details to ObjectStore spec.
		// SSE-KMS and SSE-S3 are mutually exclusive so configure one or the other.
		if kmsConfigMap != nil {
			// IBMKeyProtect and Thales are unsupported KMS providers for RGW, skip entirely
			if kmsConfigMap.Data[KMSProviderKey] == IbmKeyProtectKMSProvider ||
				kmsConfigMap.Data[KMSProviderKey] == ThalesKMSProvider {
				r.Log.Info("IBMKeyProtect/Thales as KMS provider is unsupported for RGW, skipping")
				continue
			}

			// Set default KMS_PROVIDER, refer https://issues.redhat.com/browse/RHSTOR-1963
			if _, ok := kmsConfigMap.Data[KMSProviderKey]; !ok {
				kmsConfigMap.Data[KMSProviderKey] = VaultKMSProvider
			}

			switch kmsConfigMap.Data[VaultRGWAuthMethodKey] {
			case "":
				// VAULT_RGW_AUTH_METHOD not set, skip RGW encryption
				r.Log.Info("VAULT_RGW_AUTH_METHOD not set, skipping RGW encryption configuration")
			case VaultAgentAuthMethod:
				if r.images.VaultAgent == "" {
					r.Log.Info("VAULT_AGENT_IMAGE not set, skipping SSE-S3 Vault Agent configuration for RGW")
					continue
				}
				// Configure SSE-S3 with Vault Agent auth.
				// RGW connects to the ODF-managed Vault Agent service instead of directly to Vault.
				obj.Spec.Security = &cephv1.ObjectStoreSecuritySpec{
					ServerSideEncryptionS3: cephv1.KeyManagementServiceSpec{
						ConnectionDetails: map[string]string{
							"KMS_PROVIDER":        VaultKMSProvider,
							"VAULT_ADDR":          VaultAgentServiceURL(initData.Namespace),
							"VAULT_AUTH_METHOD":   VaultAgentAuthMethod,
							"VAULT_SECRET_ENGINE": "transit",
						},
					},
				}
			case VaultTokenAuthMethod:
				// Configure SSE-KMS with token auth
				rgwConnDetails := make(map[string]string)
				for key, value := range kmsConfigMap.Data {
					if key == "VAULT_BACKEND_PATH" || key == "VAULT_BACKEND" {
						continue
					}
					rgwConnDetails[key] = value
				}
				rgwConnDetails["VAULT_SECRET_ENGINE"] = "transit"
				obj.Spec.Security = &cephv1.ObjectStoreSecuritySpec{
					SecuritySpec: cephv1.SecuritySpec{
						KeyManagementService: cephv1.KeyManagementServiceSpec{
							ConnectionDetails: rgwConnDetails,
							TokenSecretName:   KMSTokenSecretName,
						},
					},
				}
			default:
				return nil, fmt.Errorf("unsupported %s value %q, must be %q or %q",
					VaultRGWAuthMethodKey, kmsConfigMap.Data[VaultRGWAuthMethodKey], VaultAgentAuthMethod, VaultTokenAuthMethod)
			}
		}

		// set RGW readAffinity to `localize` in case of non-portable OSDs.
		if hasNonPortableOSD(initData) {
			obj.Spec.Gateway.ReadAffinity = &cephv1.RgwReadAffinity{Type: "localize"}
		}

		// Enable STS for RGW via rgwConfig and rgwSecretConfig
		if initData.Spec.ManagedResources.CephObjectStores.EnableSTS {
			if err := r.setSTSOptions(obj, initData); err != nil {
				r.Log.Error(err, "Failed to set STS options for CephObjectStore.", "CephObjectStore", klog.KRef(obj.Namespace, obj.Name))
				return nil, err
			}
		}

		if obj.Spec.Security == nil {
			obj.Spec.Security = &cephv1.ObjectStoreSecuritySpec{}
		}
		if cfg, found := ocstlsv1.GetConfigForServer(tlsProfile, "rook.io", ""); found {
			if err := ocstlsv1.ValidateTLSConfig(cfg); err != nil {
				return nil, err
			}

			osstls := ocstlsv1.OpenSSLConfigFrom(ocstlsv1.GetGoTLSConfig(cfg))
			switch osstls.Protocol {
			case "TLSv1.3":
				if obj.Spec.Security.SslOptions == nil {
					obj.Spec.Security.SslOptions = &cephv1.SslOptionsSpec{}
				}
				// rook by default disables older versions except 1.2 and we explicitly disable it
				// making 1.3 as the default
				obj.Spec.Security.SslOptions.SSLv2 = ptr.To(false)
			}
			obj.Spec.Security.Ciphers = osstls.Ciphers
			obj.Spec.Security.TlsGroups = osstls.Groups
		} else {
			// From https://docs.openssl.org/master/man3/SSL_CTX_set1_curves/#description
			// The DEFAULT list selects X25519MLKEM768 as one of the predicted keyshares.
			// Unlike Golang or Nginx servers OpenSSL needs explicit setting to enable hybrid key exchange
			// and DEFAULT here would include one along with other classic groups without any further setting.
			obj.Spec.Security.Ciphers = nil
			obj.Spec.Security.TlsGroups = []string{"DEFAULT"}
			obj.Spec.Security.SslOptions = nil
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

func hasNonPortableOSD(sc *ocsv1.StorageCluster) bool {
	for _, deviceSet := range sc.Spec.StorageDeviceSets {
		if deviceSet.Portable {
			return false
		}
	}
	return true
}

// when using non default hostNetwork Object Store should run on hostNetwork and use different port to avoid port collision
func getRGWPort(sc *ocsv1.StorageCluster) int32 {
	if sc.Spec.ManagedResources.CephObjectStores.DisableHttp {
		return 0
	} else if sc.Spec.ManagedResources.CephObjectStores.GatewayPort != 0 {
		return int32(sc.Spec.ManagedResources.CephObjectStores.GatewayPort)
	} else if util.ShouldUseHostNetworking(sc) {
		return 50080
	}
	return 80
}

// when using non default hostNetwork Object Store should run on hostNetwork and use different secure port to avoid port collision
func getRGWSecurePort(sc *ocsv1.StorageCluster) int32 {
	if sc.Spec.ManagedResources.CephObjectStores.GatewaySecurePort != 0 {
		return int32(sc.Spec.ManagedResources.CephObjectStores.GatewaySecurePort)
	} else if util.ShouldUseHostNetworking(sc) {
		return 50443
	}
	return 443
}

// generateRandomSTSKey generates a cryptographically secure random alphanumeric key for STS
// RGW requires the STS key to be alphanumeric (letters and numbers only)
// Returns a 16-character alphanumeric string suitable for use as rgw_sts_key
func generateRandomSTSKey() (string, error) {
	// Character set: alphanumeric only (a-z, A-Z, 0-9)
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const charsetLen = len(charset)

	// Generate random alphanumeric string
	key := make([]byte, stsKeyLen)
	randomBytes := make([]byte, stsKeyLen)

	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Map random bytes to alphanumeric characters
	for i, b := range randomBytes {
		key[i] = charset[int(b)%charsetLen]
	}

	return string(key), nil
}

// STS Options via rgwConfig and rgwSecretConfig
// Create k8s secret containing sts key
func (r *StorageClusterReconciler) setSTSOptions(obj *cephv1.CephObjectStore, sc *ocsv1.StorageCluster) error {
	// Set rgw_s3_auth_use_sts to true in rgwConfig
	if obj.Spec.Gateway.RgwConfig == nil {
		obj.Spec.Gateway.RgwConfig = make(map[string]string)
	}
	obj.Spec.Gateway.RgwConfig["rgw_s3_auth_use_sts"] = "true"

	// Create secret for STS key
	secretName := fmt.Sprintf("sts-key-%s", obj.Name)
	secretKeyName := "rgw_sts_key"

	// Generate a cryptographically secure random STS key (16 bytes = 128 bits)
	stsKey, err := generateRandomSTSKey()
	if err != nil {
		return fmt.Errorf("failed to generate STS key: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: obj.Namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			secretKeyName: []byte(stsKey),
		},
	}

	// Set owner reference to the StorageCluster
	if err := controllerutil.SetControllerReference(sc, secret, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for STS secret: %w", err)
	}

	// Create or update the secret
	existingSecret := &corev1.Secret{}
	err = r.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: obj.Namespace}, existingSecret)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Creating STS secret for CephObjectStore.", "Secret", klog.KRef(secret.Namespace, secret.Name), "CephObjectStore", klog.KRef(obj.Namespace, obj.Name))
			if err := r.Create(context.TODO(), secret); err != nil {
				return fmt.Errorf("failed to create STS secret: %w", err)
			}
		} else {
			return fmt.Errorf("failed to get STS secret: %w", err)
		}
	} else {
		r.Log.Info("STS secret already exists for CephObjectStore.", "Secret", klog.KRef(secret.Namespace, secret.Name), "CephObjectStore", klog.KRef(obj.Namespace, obj.Name))
	}

	// Set rgw_sts_key in RgwConfigFromSecret with SecretKeySelector
	if obj.Spec.Gateway.RgwConfigFromSecret == nil {
		obj.Spec.Gateway.RgwConfigFromSecret = make(map[string]corev1.SecretKeySelector)
	}
	obj.Spec.Gateway.RgwConfigFromSecret["rgw_sts_key"] = corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{
			Name: secretName,
		},
		Key: secretKeyName,
	}

	return nil
}
