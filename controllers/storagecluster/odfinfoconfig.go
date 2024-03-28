package storagecluster

import (
	"fmt"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"
	"strings"
	"sync"
)

type ConnectedClient struct {
	NamespacedName types.NamespacedName `yaml:"metadata"`
	ClusterID      string               `yaml:"clusterId"`
}
type InfoStorageCluster struct {
	NamespacedName          types.NamespacedName `yaml:"metadata"`
	StorageProviderEndpoint string               `yaml:"storageProviderEndpoint"`
	CephClusterFSID         string               `yaml:"cephClusterFSID"`
}

type OdfInfoData struct {
	OdfVersion          string             `yaml:"odfVersion"`
	OdfDeploymentType   string             `yaml:"odfDeploymentType"`
	Clients             []ConnectedClient  `yaml:"clients"`
	StorageCluster      InfoStorageCluster `yaml:"storageCluster"`
	StorageClusterCount int                `yaml:"storageClusterCount"`
	StorageSystemName   string             `yaml:"storageSystemName"`
	IsDROptimized       bool               `yaml:"isDROptimized"`
}

const (
	OdfInfoKeyName            = "config.yaml"
	OdfDeploymentTypeExternal = "external"
	OdfDeploymentTypeInternal = "internal"
	RookCephMonSecretName     = "rook-ceph-mon"
	FsidKey                   = "fsid"
	OdfOperatorNamePrefix     = "odf-operator"
	OdfInfoConfigMapName      = "odf-info"
	OdfInfoMapKind            = "ConfigMap"
)

type odfInfoConfig struct{}

var mutex sync.RWMutex

// ensureCreated ensures that a ConfigMap resource exists with its Spec in
// the desired state.
func (obj *odfInfoConfig) ensureCreated(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	operatorNamespace, err := util.GetOperatorNamespace()
	if err != nil {
		return reconcile.Result{}, err
	}

	odfInfoConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      OdfInfoConfigMapName,
			Namespace: operatorNamespace,
		},
	}

	mutex.Lock()
	defer mutex.Unlock()
	_, err = ctrl.CreateOrUpdate(r.ctx, r.Client, odfInfoConfigMap, func() error {
		if err := ctrl.SetControllerReference(sc, odfInfoConfigMap, r.Scheme); err != nil {
			return err
		}
		r.Log.Info("Creating or updating odf-info configmap", OdfInfoMapKind, klog.KRef(sc.Namespace, OdfInfoConfigMapName))
		odfInfoData, configErr := getOdfInfoData(r, sc)
		if configErr != nil {
			return fmt.Errorf("failed to get ODF info config data: %w", configErr)
		}
		odfInfoKey := obj.getOdfInfoKeyName(sc)
		// Creates or appends to the data map
		odfInfoConfigMap.Data[odfInfoKey] = odfInfoData
		return nil
	})
	if err != nil {
		r.Log.Error(err, "failed to create or update odf-info config", OdfInfoMapKind, klog.KRef(sc.Namespace, OdfInfoConfigMapName))
		return reconcile.Result{}, fmt.Errorf("failed to create or update odf-info config: %w", err)
	}
	return reconcile.Result{}, nil
}

func (obj *odfInfoConfig) getOdfInfoKeyName(sc *ocsv1.StorageCluster) string {
	return sc.Namespace + "/" + sc.Name + "." + OdfInfoKeyName
}

// ensureDeleted is dummy func for the odfInfoConfig
func (obj *odfInfoConfig) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	operatorNamespace, err := util.GetOperatorNamespace()
	if err != nil {
		return reconcile.Result{}, err
	}
	var odfInfoConfigMap corev1.ConfigMap
	if err = r.Client.Get(r.ctx, types.NamespacedName{Namespace: operatorNamespace}, &odfInfoConfigMap); err != nil {
		return reconcile.Result{}, err
	}

	mutex.Lock()
	defer mutex.Unlock()
	if len(odfInfoConfigMap.Data) > 1 {
		delete(odfInfoConfigMap.Data, obj.getOdfInfoKeyName(sc))
	} else {
		if err = r.Client.Delete(r.ctx, &odfInfoConfigMap); err != nil {
			r.Log.Error(err, "Failed to delete odf-info.", "ConfigMap", klog.KRef(odfInfoConfigMap.Namespace, odfInfoConfigMap.Name))
			return reconcile.Result{}, fmt.Errorf("failed to delete odf-info %v: %v", odfInfoConfigMap.Name, err)
		}
	}
	return reconcile.Result{}, nil
}

func getOdfInfoData(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (string, error) {
	var odfVersion, cephFSId string
	var err error
	if odfVersion, err = getOdfVersion(r, sc); err != nil {
		return "", err
	}
	if cephFSId, err = getCephFsid(r, sc); err != nil {
		return "", err
	}
	var odfDeploymentType string
	if sc.Spec.ExternalStorage.Enable {
		odfDeploymentType = OdfDeploymentTypeExternal
	} else {
		odfDeploymentType = OdfDeploymentTypeInternal
	}
	var isDROptimized = false
	// Set isDROptmized to "false" in case of external clusters as we currently don't have to way to determine
	// if external cluster OSDs are using bluestore-rdr
	if !sc.Spec.ExternalStorage.Enable {
		if isDROptimized, err = getIsDROptimized(r, sc); err != nil {
			return "", err

		}
	}
	var storageSystemName string
	if storageSystemName, err = getStorageSystemName(sc); err != nil {
		return "", err
	}
	storageClusterCount := len(r.clusters.GetStorageClusters())

	var data = OdfInfoData{
		OdfVersion:          odfVersion,
		OdfDeploymentType:   odfDeploymentType,
		StorageClusterCount: storageClusterCount,
		IsDROptimized:       isDROptimized,
		StorageSystemName:   storageSystemName,
		// Clients array is populated with the onboarding request's fields via server.go
		Clients: []ConnectedClient{},
		StorageCluster: InfoStorageCluster{
			NamespacedName:          types.NamespacedName{Name: sc.Name, Namespace: sc.Namespace},
			StorageProviderEndpoint: sc.Status.StorageProviderEndpoint,
			CephClusterFSID:         cephFSId,
		},
	}
	yamlData, err := yaml.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(yamlData), nil

}

func getStorageSystemName(storageCluster *ocsv1.StorageCluster) (string, error) {
	for i := range storageCluster.OwnerReferences {
		ref := &storageCluster.OwnerReferences[i]
		if ref.Kind == "StorageSystem" {
			return ref.Name, nil
		}
	}

	return "", fmt.Errorf("failed to find parent StorageSystem's name in StorageCluster %q ownerreferences, %v", storageCluster.Name, storageCluster.OwnerReferences)

}

func getIsDROptimized(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (bool, error) {
	var cephCluster rookCephv1.CephCluster
	err := r.Client.Get(r.ctx, types.NamespacedName{Name: generateNameForCephClusterFromString(storageCluster.Name), Namespace: storageCluster.Namespace}, &cephCluster)
	if err != nil {
		return false, err
	}
	if cephCluster.Status.CephStorage == nil || cephCluster.Status.CephStorage.OSD.StoreType == nil {
		return false, fmt.Errorf("cephcluster %v status does not have OSD store information, %v", cephCluster.Name, storageCluster.Name)
	}
	bluestorerdr, ok := cephCluster.Status.CephStorage.OSD.StoreType["bluestore-rdr"]
	if !ok {
		return false, nil
	}
	total := getOsdCount(storageCluster, r.serverVersion)
	if bluestorerdr < total {
		return false, nil
	}
	return true, nil
}

func getOdfVersion(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (string, error) {
	var csvs operatorsv1alpha1.ClusterServiceVersionList
	err := r.Client.List(r.ctx, &csvs, client.InNamespace(storageCluster.Namespace))
	if err != nil {
		return "", err
	}
	for _, csv := range csvs.Items {
		if strings.HasPrefix(csv.Name, OdfOperatorNamePrefix) {
			return csv.Spec.Version.String(), nil
		}
	}

	return "", fmt.Errorf("failed to find csv with prefix %q", OdfOperatorNamePrefix)
}

func getCephFsid(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (string, error) {
	var rookCephMonSecret corev1.Secret
	if err := r.Client.Get(r.ctx, types.NamespacedName{Name: RookCephMonSecretName, Namespace: storageCluster.Namespace}, &rookCephMonSecret); err != nil {
		return "", err
	}
	if val, ok := rookCephMonSecret.Data[FsidKey]; ok {
		return string(val), nil
	}

	return "", fmt.Errorf("failed to fetch ceph fsid from %q secret", RookCephMonSecretName)
}
