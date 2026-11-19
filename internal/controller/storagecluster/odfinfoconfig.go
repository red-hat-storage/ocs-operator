package storagecluster

import (
	"crypto/x509"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"

	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	odfInfoKeySuffix          = "config.yaml"
	odfDeploymentTypeExternal = "external"
	odfDeploymentTypeInternal = "internal"
	rookCephMonSecretName     = "rook-ceph-mon"
	fsidKey                   = "fsid"
	ocsOperatorNamePrefix     = "ocs-operator"
	OdfInfoConfigMapName      = "odf-info"
	odfInfoClientCertName     = "odf-info-client-cert"
	odfInfoClientCAName       = "odf-info-client-ca"
	odfInfoMapKind            = "ConfigMap"
	peerCertValidity          = util.SelfSignedCertValidity
	peerCertRenewalWindow     = 7 * 24 * time.Hour
)

type odfInfoConfig struct{}

var _ resourceManager = &odfInfoConfig{}

var mutex sync.RWMutex

// ensureCreated ensures that a ConfigMap resource exists with its Spec in
// the desired state.
func (obj *odfInfoConfig) ensureCreated(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (reconcile.Result, error) {
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
	tlsInfo, result, err := obj.createClientCertAndCA(r, operatorNamespace, storageCluster)
	if err != nil || !result.IsZero() {
		return result, err
	}

	_, err = ctrl.CreateOrUpdate(r.ctx, r.Client, odfInfoConfigMap, func() error {
		r.Log.Info("Creating or updating odf-info config map", odfInfoMapKind, client.ObjectKeyFromObject(odfInfoConfigMap))
		odfInfoKey := obj.getOdfInfoKeyName(storageCluster)

		odfInfoData, configErr := getOdfInfoData(r, storageCluster, tlsInfo)
		if configErr != nil {
			return fmt.Errorf("failed to get odf-info config map data: %v", configErr)
		}
		if odfInfoConfigMap.Data == nil {
			odfInfoConfigMap.Data = map[string]string{}
		}
		// Creates or appends to the data map
		odfInfoConfigMap.Data[odfInfoKey] = odfInfoData
		return nil
	})
	if err != nil {
		r.Log.Error(err, "failed to create or update odf-info config map", odfInfoMapKind, client.ObjectKeyFromObject(odfInfoConfigMap))
		return reconcile.Result{}, fmt.Errorf("failed to create or update odf-info config: %v", err)
	}
	return reconcile.Result{}, nil
}

func (obj *odfInfoConfig) createClientCertAndCA(r *StorageClusterReconciler, operatorNamespace string, storageCluster *ocsv1.StorageCluster) (ocsv1a1.TLSInfo, reconcile.Result, error) {
	secret := &corev1.Secret{}
	secret.Name = odfInfoClientCertName
	secret.Namespace = storageCluster.Namespace
	if err := r.Get(r.ctx, client.ObjectKeyFromObject(secret), secret); client.IgnoreNotFound(err) != nil {
		r.Log.Error(err, "Failed to get client cert secret", "Name", secret.Name)
		return ocsv1a1.TLSInfo{}, reconcile.Result{}, err
	}

	// secret content is stateful based on random data in cert and shouldn't be
	// updated in every reconcile
	if secret.UID == "" {
		if err := populateClientCertSecret(r, storageCluster, secret); err != nil {
			return ocsv1a1.TLSInfo{}, reconcile.Result{}, err
		}
		if err := r.Create(r.ctx, secret); err != nil {
			if errors.IsAlreadyExists(err) {
				return ocsv1a1.TLSInfo{}, reconcile.Result{Requeue: true}, nil
			}
			r.Log.Error(err, "Failed to create client cert secret", "Name", secret.Name)
			return ocsv1a1.TLSInfo{}, reconcile.Result{}, err
		}
	} else if util.CertNeedsRenewal(secret, peerCertRenewalWindow) {
		if err := populateClientCertSecret(r, storageCluster, secret); err != nil {
			return ocsv1a1.TLSInfo{}, reconcile.Result{}, err
		}
		if err := r.Update(r.ctx, secret); err != nil {
			r.Log.Error(err, "Failed to update client cert secret", "Name", secret.Name)
			return ocsv1a1.TLSInfo{}, reconcile.Result{}, err
		}
	}

	caCert := secret.Data["tls.crt"]
	if len(caCert) == 0 {
		return ocsv1a1.TLSInfo{}, reconcile.Result{}, fmt.Errorf("client cert secret %s has no tls.crt", secret.Name)
	}

	clientCm := &corev1.ConfigMap{}
	clientCm.Name = odfInfoClientCAName
	clientCm.Namespace = storageCluster.Namespace
	if _, err := ctrl.CreateOrUpdate(r.ctx, r.Client, clientCm, func() error {
		if clientCm.Data == nil {
			clientCm.Data = make(map[string]string)
		}
		clientCm.Data["ca.crt"] = string(caCert)
		return controllerutil.SetControllerReference(storageCluster, clientCm, r.Client.Scheme())
	}); err != nil {
		r.Log.Error(err, "Failed to ensure client CA configmap", "Name", clientCm.Name)
		return ocsv1a1.TLSInfo{}, reconcile.Result{}, err
	}

	serverCm := &corev1.ConfigMap{}
	serverCm.Name = defaults.ProviderServerCAConfigMap
	serverCm.Namespace = operatorNamespace
	if err := r.Get(r.ctx, client.ObjectKeyFromObject(serverCm), serverCm); err != nil {
		return ocsv1a1.TLSInfo{}, reconcile.Result{}, err
	}

	return ocsv1a1.TLSInfo{
		ServerSAN:       util.DefaultProviderServerSAN(defaults.OcsProviderServerName, operatorNamespace),
		ServerCMContent: serverCm.Data["service-ca.crt"],
		ClientSAN:       secret.Annotations[defaults.CertIdentityAnnotation],
		ClientCMContent: clientCm.Data["ca.crt"],
		ClientCertName:  secret.Name,
	}, reconcile.Result{}, nil
}

func populateClientCertSecret(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster, secret *corev1.Secret) error {
	clientCN := fmt.Sprintf("%s-%x", defaults.LocalPeerCNPrefix, util.FnvHash(strconv.FormatInt(time.Now().UnixNano(), 10)))
	keyUsage := x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature
	extKeyUsage := []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	keyPEM, certPEM, err := util.GetSelfSignedCert(clientCN, keyUsage, extKeyUsage)
	if err != nil {
		return fmt.Errorf("failed to generate client certificate: %v", err)
	}
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	secret.Data["tls.key"] = keyPEM
	secret.Data["tls.crt"] = certPEM
	if secret.Annotations == nil {
		secret.Annotations = make(map[string]string)
	}
	secret.Annotations[defaults.CertExpiryAnnotation] = time.Now().Add(peerCertValidity).Format(time.RFC3339)
	secret.Annotations[defaults.CertIdentityAnnotation] = clientCN
	secret.Type = corev1.SecretTypeTLS
	return controllerutil.SetControllerReference(storageCluster, secret, r.Client.Scheme())
}

func (obj *odfInfoConfig) getOdfInfoKeyName(storageCluster *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s_%s.%s", storageCluster.Namespace, storageCluster.Name, odfInfoKeySuffix)
}

// ensureDeleted is dummy func for the odfInfoConfig
func (obj *odfInfoConfig) ensureDeleted(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (reconcile.Result, error) {
	operatorNamespace, err := util.GetOperatorNamespace()
	if err != nil {
		return reconcile.Result{}, err
	}
	odfInfoConfigMap := &corev1.ConfigMap{}
	odfInfoConfigMap.Name = OdfInfoConfigMapName
	odfInfoConfigMap.Namespace = operatorNamespace
	if err = r.Get(r.ctx, client.ObjectKeyFromObject(odfInfoConfigMap), odfInfoConfigMap); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	mutex.Lock()
	defer mutex.Unlock()
	if len(odfInfoConfigMap.Data) > 1 {
		odfInfoKeyName := obj.getOdfInfoKeyName(storageCluster)
		delete(odfInfoConfigMap.Data, odfInfoKeyName)
		if err = r.Update(r.ctx, odfInfoConfigMap); err != nil && !errors.IsNotFound(err) {
			r.Log.Error(err, "Failed to update odf-info config map with deleted key.", "ConfigMap", client.ObjectKeyFromObject(odfInfoConfigMap), "Key", odfInfoKeyName)
			return reconcile.Result{}, fmt.Errorf("failed to delete key %v in odf-info %v: %v", odfInfoKeyName, odfInfoConfigMap.Name, err)
		}
	} else {
		if err = r.Delete(r.ctx, odfInfoConfigMap); err != nil && !errors.IsNotFound(err) {
			r.Log.Error(err, "Failed to delete odf-info config map.", "ConfigMap", client.ObjectKeyFromObject(odfInfoConfigMap))
			return reconcile.Result{}, fmt.Errorf("failed to delete odf-info %v: %v", odfInfoConfigMap.Name, err)
		}
	}
	return reconcile.Result{}, nil
}

func getOdfInfoData(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster, tlsInfo ocsv1a1.TLSInfo) (string, error) {
	ocsVersion, err := getOcsVersion(r)
	if err != nil {
		return "", err
	}
	cephFSId, err := getCephFsid(r, storageCluster)
	if err != nil {
		return "", err
	}

	odfDeploymentType := odfDeploymentTypeExternal
	if !storageCluster.Spec.ExternalStorage.Enable {
		odfDeploymentType = odfDeploymentTypeInternal
	}

	connectedClients, err := getConnectedClients(r, storageCluster)
	if err != nil {
		return "", err
	}

	infoCephBlockPools, err := getInfoCephBlockPools(r, storageCluster)
	if err != nil {
		return "", err
	}

	annotations := map[string]string{}
	for key, value := range storageCluster.GetAnnotations() {
		parts := strings.Split(key, "/")
		if len(parts) == 2 && strings.HasSuffix(parts[0], "ocs.openshift.io") {
			annotations[key] = value
		}
	}

	data := ocsv1a1.OdfInfoData{
		Version:        ocsVersion,
		DeploymentType: odfDeploymentType,
		Clients:        connectedClients,
		StorageCluster: ocsv1a1.InfoStorageCluster{
			NamespacedName:          client.ObjectKeyFromObject(storageCluster),
			StorageProviderEndpoint: storageCluster.Status.StorageProviderEndpoint,
			CephClusterFSID:         cephFSId,
			StorageClusterUID:       string(storageCluster.UID),
			Annotations:             annotations,
			InfoCephBlockPools:      infoCephBlockPools,
		},
		TLSInfo: tlsInfo,
	}
	yamlData, err := yaml.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(yamlData), nil

}

func getConnectedClients(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) ([]ocsv1a1.ConnectedClient, error) {
	storageConsumers := &ocsv1a1.StorageConsumerList{}
	err := r.List(r.ctx, storageConsumers, client.InNamespace(storageCluster.Namespace))
	if err != nil {
		return nil, err
	}
	connectedClients := make([]ocsv1a1.ConnectedClient, 0, len(storageConsumers.Items))

	for storageConsumerIdx := range storageConsumers.Items {
		storageConsumer := &storageConsumers.Items[storageConsumerIdx]
		if storageConsumer.Status.Client == nil {
			continue
		}
		clusterID := storageConsumer.Status.Client.ClusterID
		name := storageConsumer.Status.Client.Name
		newConnectedClient := ocsv1a1.ConnectedClient{
			Name:      name,
			ClusterID: clusterID,
			ClientID:  storageConsumer.Status.Client.ID,
		}
		connectedClients = append(connectedClients, newConnectedClient)
	}

	return connectedClients, nil
}

func getOcsVersion(r *StorageClusterReconciler) (string, error) {
	var csvs operatorsv1alpha1.ClusterServiceVersionList
	err := r.List(r.ctx, &csvs, client.InNamespace(r.OperatorNamespace))
	if err != nil {
		return "", err
	}

	csv := util.Find(csvs.Items, func(csv *operatorsv1alpha1.ClusterServiceVersion) bool {
		return strings.HasPrefix(csv.Name, ocsOperatorNamePrefix)
	})
	if csv == nil {
		return "", fmt.Errorf("failed to find csv with prefix %q", ocsOperatorNamePrefix)
	}
	return csv.Spec.Version.String(), nil
}

func getCephFsid(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (string, error) {
	rookCephMonSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rookCephMonSecretName,
			Namespace: storageCluster.Namespace,
		},
	}

	if err := r.Get(r.ctx, client.ObjectKeyFromObject(rookCephMonSecret), rookCephMonSecret); err != nil {
		return "", err
	}
	var val []byte
	var ok bool
	if val, ok = rookCephMonSecret.Data[fsidKey]; !ok {
		return "", fmt.Errorf("failed to fetch ceph fsid from %q secret", rookCephMonSecretName)
	}

	return string(val), nil
}

func getInfoCephBlockPools(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) ([]ocsv1a1.InfoCephBlockPool, error) {
	cephBlockPoolsList := &rookCephv1.CephBlockPoolList{}
	if err := r.List(r.ctx, cephBlockPoolsList, client.InNamespace(storageCluster.Namespace)); err != nil {
		return nil, err
	}
	infoCephBlockPools := []ocsv1a1.InfoCephBlockPool{}

	for idx := range cephBlockPoolsList.Items {
		cephBlockPool := &cephBlockPoolsList.Items[idx]
		if forInternalUseOnly, _ := strconv.ParseBool(cephBlockPool.GetLabels()[util.ForInternalUseOnlyLabelKey]); !forInternalUseOnly {
			infoCephBlockPools = append(infoCephBlockPools, ocsv1a1.InfoCephBlockPool{
				Name:          cephBlockPool.Name,
				MirrorEnabled: cephBlockPool.Spec.Mirroring.Enabled,
			})
		}
	}

	return infoCephBlockPools, nil
}
