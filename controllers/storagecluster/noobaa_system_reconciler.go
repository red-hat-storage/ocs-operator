package storagecluster

import (
	"context"
	"fmt"
	"strings"

	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	objectreferencesv1 "github.com/openshift/custom-resource-status/objectreferences/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	statusutil "github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/reference"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	MonitoringNamespace = "openshift-monitoring"
)

type ocsNoobaaSystem struct{}

func (obj *ocsNoobaaSystem) ensureCreated(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	// skip noobaa reconcile if it is not requested from same namespace as operator
	if sc.Namespace != r.OperatorNamespace {
		return reconcile.Result{}, nil
	}

	var err error
	var reconcileStrategy ReconcileStrategy

	// Everything other than ReconcileStrategyIgnore means we reconcile
	if sc.Spec.MultiCloudGateway != nil {
		reconcileStrategy = ReconcileStrategy(sc.Spec.MultiCloudGateway.ReconcileStrategy)
		if reconcileStrategy == ReconcileStrategyIgnore {
			return reconcile.Result{}, nil
		}
	}

	if !r.IsNoobaaStandalone {
		// find cephCluster
		foundCeph := &rookCephv1.CephCluster{}
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: statusutil.GenerateNameForCephCluster(sc), Namespace: sc.Namespace}, foundCeph)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Waiting on Ceph Cluster to be created before starting Noobaa.", "CephCluster", klog.KRef(sc.Namespace, statusutil.GenerateNameForCephCluster(sc)))
				return reconcile.Result{}, nil
			}
			r.Log.Error(err, "Failed to retrieve Ceph Cluster.", "CephCluster", klog.KRef(sc.Namespace, statusutil.GenerateNameForCephCluster(sc)))
			return reconcile.Result{}, err
		}
		if !sc.Spec.ExternalStorage.Enable {
			if foundCeph.Status.State != rookCephv1.ClusterStateCreated {
				r.Log.Info("Waiting on Ceph Cluster to initialize before starting Noobaa.", "CephCluster", klog.KRef(sc.Namespace, statusutil.GenerateNameForCephCluster(sc)))
				return reconcile.Result{}, nil
			}
		} else {
			if foundCeph.Status.State != rookCephv1.ClusterStateConnected {
				r.Log.Info("Waiting for the External Ceph Cluster to be connected before starting Noobaa.", "CephCluster", klog.KRef(sc.Namespace, statusutil.GenerateNameForCephCluster(sc)))
				return reconcile.Result{}, nil
			}
		}
	}

	// Take ownership over the noobaa object
	nb := &nbv1.NooBaa{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NooBaa",
			APIVersion: "noobaa.io/v1alpha1'",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "noobaa",
			Namespace: sc.Namespace,
		},
		Spec: nbv1.NooBaaSpec{
			Labels: nbv1.LabelsSpec{
				"monitoring": getNooBaaMonitoringLabels(*sc),
			},
		},
	}
	err = controllerutil.SetControllerReference(sc, nb, r.Scheme)
	if err != nil {
		r.Log.Error(err, "Unable to set controller reference for Noobaa.", "Noobaa", klog.KRef(nb.Namespace, nb.Name))
		return reconcile.Result{}, err
	}

	// Reconcile the noobaa state, creating or updating if needed
	_, err = controllerutil.CreateOrUpdate(context.TODO(), r.Client, nb, func() error {
		return r.setNooBaaDesiredState(nb, sc)
	})
	if err != nil {
		r.Log.Error(err, "Failed to create or update NooBaa system.", "Noobaa", klog.KRef(nb.Namespace, nb.Name))
		return reconcile.Result{}, err
	}
	// Need to happen after the noobaa CR update was confirmed
	sc.Status.Images.NooBaaCore.ActualImage = *nb.Spec.Image
	sc.Status.Images.NooBaaDB.ActualImage = *nb.Spec.DBSpec.DBImage

	objectRef, err := reference.GetReference(r.Scheme, nb)
	if err != nil {
		return reconcile.Result{}, err
	}
	err = objectreferencesv1.SetObjectReference(&sc.Status.RelatedObjects, *objectRef)
	if err != nil {
		return reconcile.Result{}, err
	}

	statusutil.MapNoobaaNegativeConditions(&r.conditions, nb)
	return reconcile.Result{}, nil
}

func getNooBaaMonitoringLabels(sc ocsv1.StorageCluster) map[string]string {
	labels := make(map[string]string)
	if sc.Spec.Monitoring != nil && sc.Spec.Monitoring.Labels != nil {
		labels = sc.Spec.Monitoring.Labels
	}
	if sc.Spec.MultiCloudGateway != nil &&
		ReconcileStrategy(sc.Spec.MultiCloudGateway.ReconcileStrategy) == ReconcileStrategyStandalone {
		labels["noobaa.io/managedBy"] = sc.Name
	}
	return labels
}

func (r *StorageClusterReconciler) setNooBaaDesiredState(nb *nbv1.NooBaa, sc *ocsv1.StorageCluster) error {
	coreResources := defaults.GetDaemonResources("noobaa-core", sc.Spec.Resources)
	dbResources := defaults.GetDaemonResources("noobaa-db", sc.Spec.Resources)
	dBVolumeResources := defaults.GetDaemonResources("noobaa-db-vol", sc.Spec.Resources)
	endpointResources := defaults.GetDaemonResources("noobaa-endpoint", sc.Spec.Resources)

	nb.Labels = map[string]string{
		"app": "noobaa",
	}

	util.AddAnnotation(nb, "MulticloudObjectGatewayProviderMode", "true")

	// Set DB information inside DBSpec and not directly in NooBaaSpec
	// DBSpec is used for deploying a posgres client, while the DB fields were used for the single DB instance.
	// The fields in NooBaaSpec should be left unmodified, to keep the old DB up until data is imported into the new DB cluster.
	dbMinVolumeSize := dBVolumeResources.Requests.Storage().String()
	nb.Spec.DBSpec = &nbv1.NooBaaDBSpec{
		DBImage:         &r.images.NooBaaDB,
		DBMinVolumeSize: dbMinVolumeSize,
	}

	if !r.IsNoobaaStandalone {
		storageClassName := util.GenerateNameForCephBlockPoolStorageClass(sc)

		if sc.Spec.ExternalStorage.Enable {
			externalStorageClassName, err := r.GenerateNameForExternalModeCephBlockPoolSC(nb)
			if err != nil {
				return err
			}
			storageClassName = externalStorageClassName
		}

		nb.Spec.DBSpec.DBStorageClass = &storageClassName
		nb.Spec.PVPoolDefaultStorageClass = &storageClassName
	}

	nb.Spec.CoreResources = &coreResources
	nb.Spec.DBSpec.DBResources = &dbResources

	component := "noobaa-standalone"
	// fallback to 'noobaa-core' if either the cluster is not standalone or if "noobaa-standalone" placement is not found
	_, ok := sc.Spec.Placement[rookCephv1.KeyType(component)]
	if !r.IsNoobaaStandalone || !ok {
		component = "noobaa-core"
	}
	placement := getPlacement(sc, component)

	nb.Spec.Tolerations = placement.Tolerations

	if !r.IsNoobaaStandalone || ok {
		// Add affinity if not in noobaa-standalone mode or if placement is specified
		nb.Spec.Affinity = &nbv1.AffinitySpec{NodeAffinity: placement.NodeAffinity}
		topologyMap := sc.Status.NodeTopologies
		if topologyMap != nil {
			topologyKey := getFailureDomain(sc)
			topologyKey, _ = topologyMap.GetKeyValues(topologyKey)
			nb.Spec.Affinity.TopologyKey = topologyKey
		}
	} else if nb.Spec.Affinity != nil {
		// Clear the affinity if it was set previously to handle upgrades
		nb.Spec.Affinity = nil
	}

	nb.Spec.Image = &r.images.NooBaaCore

	// Default endpoint spec.
	nb.Spec.Endpoints = &nbv1.EndpointsSpec{
		MinCount:               1,
		MaxCount:               2,
		AdditionalVirtualHosts: []string{},

		// TODO: After spec.resources["noobaa-endpoint"] is declared obesolete this
		// definition should hold a constant value. and should not be read from
		// GetDaemonResources()
		Resources: &endpointResources,
	}
	// Add autoscale spec for noobaa CR
	nb.Spec.Autoscaler = nbv1.AutoscalerSpec{
		AutoscalerType:      nbv1.AutoscalerTypeHPAV2,
		PrometheusNamespace: MonitoringNamespace,
	}

	// Override with MCG options specified in the storage cluster spec
	if sc.Spec.MultiCloudGateway != nil {
		nb.Spec.DenyHTTP = sc.Spec.MultiCloudGateway.DenyHTTP

		dbStorageClass := sc.Spec.MultiCloudGateway.DbStorageClassName
		if dbStorageClass != "" {
			nb.Spec.DBSpec.DBStorageClass = &dbStorageClass
			nb.Spec.PVPoolDefaultStorageClass = &dbStorageClass
		}
		if sc.Spec.MultiCloudGateway.Endpoints != nil {
			epSpec := sc.Spec.MultiCloudGateway.Endpoints

			nb.Spec.Endpoints.MinCount = epSpec.MinCount
			nb.Spec.Endpoints.MaxCount = epSpec.MaxCount
			if epSpec.AdditionalVirtualHosts != nil {
				nb.Spec.Endpoints.AdditionalVirtualHosts = epSpec.AdditionalVirtualHosts
			}
			if epSpec.Resources != nil {
				nb.Spec.Endpoints.Resources = epSpec.Resources
			}
		}
		nb.Spec.DisableLoadBalancerService = sc.Spec.MultiCloudGateway.DisableLoadBalancerService

		nb.Spec.DisableRoutes = sc.Spec.MultiCloudGateway.DisableRoutes

		if sc.Spec.MultiCloudGateway.ExternalPgConfig != nil && sc.Spec.MultiCloudGateway.ExternalPgConfig.PGSecretName != "" {
			nb.Spec.ExternalPgSecret = &corev1.SecretReference{Name: sc.Spec.MultiCloudGateway.ExternalPgConfig.PGSecretName, Namespace: sc.Namespace}
			nb.Spec.ExternalPgSSLUnauthorized = sc.Spec.MultiCloudGateway.ExternalPgConfig.AllowSelfSignedCerts
			nb.Spec.ExternalPgSSLRequired = sc.Spec.MultiCloudGateway.ExternalPgConfig.EnableTLS
			if sc.Spec.MultiCloudGateway.ExternalPgConfig.EnableTLS && sc.Spec.MultiCloudGateway.ExternalPgConfig.TLSSecretName != "" {
				nb.Spec.ExternalPgSSLSecret = &corev1.SecretReference{Name: sc.Spec.MultiCloudGateway.ExternalPgConfig.TLSSecretName, Namespace: sc.Namespace}
			}

			if !sc.Spec.MultiCloudGateway.ExternalPgConfig.EnableTLS && sc.Spec.MultiCloudGateway.ExternalPgConfig.TLSSecretName != "" {
				return fmt.Errorf("Failed to create Noobaa system: tlsSecretName is a non-nil value while enableTls is disabled. Please set spec.multiCloudGateway.externalPgConfig.enableTls to true and retry again. enableTls: %v and tlsSecretName: %v", sc.Spec.MultiCloudGateway.ExternalPgConfig.EnableTLS, sc.Spec.MultiCloudGateway.ExternalPgConfig.TLSSecretName)
			}
		}

	}

	// Add KMS details to Noobaa spec, only if
	// KMS is enabled, along with
	// ClusterWide encryption/any deviceSet Encryption OR in a StandAlone Noobaa cluster mode
	// PS: sc.Spec.Encryption.Enable field is deprecated and added for backward compatibility
	if sc.Spec.Encryption.KeyManagementService.Enable &&
		(util.IsClusterOrDeviceSetEncrypted(sc) || r.IsNoobaaStandalone) {
		if kmsConfig, err := util.GetKMSConfigMap(defaults.KMSConfigMapName, sc, r.Client); err != nil {
			return err
		} else if kmsConfig != nil {
			// Set default KMS_PROVIDER, if it is empty. Possible values are: vault, ibmkeyprotect, kmip.
			if kmsConfig.Data["KMS_PROVIDER"] == "" {
				kmsConfig.Data["KMS_PROVIDER"] = VaultKMSProvider
			}
			var kmsProviderName = kmsConfig.Data["KMS_PROVIDER"]
			// vault as a KMS service provider
			if kmsProviderName == VaultKMSProvider {
				// Set default VAULT_AUTH_METHOD. Possible values are: token, kubernetes.
				if kmsConfig.Data["VAULT_AUTH_METHOD"] == "" {
					kmsConfig.Data["VAULT_AUTH_METHOD"] = VaultTokenAuthMethod
				}
				// Set TokenSecretName only for vault token based auth method
				if kmsConfig.Data["VAULT_AUTH_METHOD"] == VaultTokenAuthMethod {
					// Secret is created by UI in "openshift-storage" namespace
					nb.Spec.Security.KeyManagementService.TokenSecretName = KMSTokenSecretName
				}
			} else if kmsProviderName == IbmKeyProtectKMSProvider || kmsProviderName == ThalesKMSProvider {
				// Secret is created by UI in "openshift-storage" namespace
				nb.Spec.Security.KeyManagementService.TokenSecretName = kmsConfig.Data[kmsProviderSecretKeyMap[kmsProviderName]]
			}
			nb.Spec.Security.KeyManagementService.ConnectionDetails = kmsConfig.Data
		}
	}

	isEnabled, rotationSchedule := statusutil.GetKeyRotationSpec(sc)
	nb.Spec.Security.KeyManagementService.EnableKeyRotation = isEnabled
	nb.Spec.Security.KeyManagementService.Schedule = rotationSchedule
	return nil
}

// ensureDeleted Delete noobaa system in the namespace
func (obj *ocsNoobaaSystem) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	// skip noobaa reconcile if it is not requested from same namespace as operator
	if sc.Namespace != r.OperatorNamespace {
		return reconcile.Result{}, nil
	}

	// Delete only if this is being managed by the OCS operator
	if sc.Spec.MultiCloudGateway != nil {
		reconcileStrategy := ReconcileStrategy(sc.Spec.MultiCloudGateway.ReconcileStrategy)
		if reconcileStrategy == ReconcileStrategyIgnore {
			return reconcile.Result{}, nil
		}
	}
	noobaa := &nbv1.NooBaa{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: "noobaa", Namespace: sc.Namespace}, noobaa)
	if err != nil {
		if errors.IsNotFound(err) {
			pvcs := &corev1.PersistentVolumeClaimList{}
			opts := []client.ListOption{
				client.InNamespace(sc.Namespace),
				client.MatchingLabels(map[string]string{"app": "noobaa"}),
			}
			err = r.Client.List(context.TODO(), pvcs, opts...)
			if err != nil {
				return reconcile.Result{}, err
			}
			if len(pvcs.Items) > 0 {
				return reconcile.Result{}, fmt.Errorf("Uninstall: Waiting on NooBaa PVCs to be deleted")
			}
			r.Log.Info("Uninstall: NooBaa and noobaa-db PVC not found.")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("Uninstall: Failed to retrieve NooBaa system: %v", err)
	}

	isOwned := false
	for _, ref := range noobaa.GetOwnerReferences() {
		if ref.Name == sc.Name && ref.Kind == sc.Kind {
			isOwned = true
			break
		}
	}
	if !isOwned {
		// if the noobaa found is not owned by our storagecluster, we skip it from deletion.
		r.Log.Info("Uninstall: NooBaa object found, but ownerReference not set to storagecluster. Skipping Deletion.", "Noobaa", klog.KRef(noobaa.Namespace, noobaa.Name))
		return reconcile.Result{}, nil
	}

	if noobaa.GetDeletionTimestamp().IsZero() {
		r.Log.Info("Uninstall: Deleting NooBaa system.", "Noobaa", klog.KRef(noobaa.Namespace, noobaa.Name))
		err = r.Client.Delete(context.TODO(), noobaa)
		if err != nil {
			r.Log.Error(err, "Uninstall: Failed to delete NooBaa system.", "Noobaa", klog.KRef(noobaa.Namespace, noobaa.Name))
			return reconcile.Result{}, fmt.Errorf("uninstall: Failed to delete NooBaa system %v : %v", noobaa.ObjectMeta.Name, err)
		}
	}
	return reconcile.Result{}, fmt.Errorf("uninstall: Waiting on NooBaa system %v to be deleted", noobaa.ObjectMeta.Name)
}

func (r *StorageClusterReconciler) GenerateNameForExternalModeCephBlockPoolSC(nb *nbv1.NooBaa) (string, error) {
	var storageClassName string

	if nb.Spec.DBStorageClass != nil {
		storageClassName = *nb.Spec.DBStorageClass
	} else {
		// list all storage classes and pick the first one
		// whose provisioner suffix matches with `rbd.csi.ceph.com` suffix
		storageClasses := &storagev1.StorageClassList{}
		err := r.Client.List(r.ctx, storageClasses)
		if err != nil {
			r.Log.Error(err, "Failed to list storage classes")
			return "", err
		}

		for _, sc := range storageClasses.Items {
			if strings.HasSuffix(sc.Provisioner, "rbd.csi.ceph.com") {
				storageClassName = sc.Name
				break
			}
		}
	}

	if storageClassName == "" {
		return "", fmt.Errorf("no storage class found with provisioner suffix `rbd.csi.ceph.com`")
	}

	return storageClassName, nil
}
