package storagecluster

import (
	"context"
	"crypto/sha512"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	externalClusterDetailsSecret          = "rook-ceph-external-cluster-details"
	externalClusterDetailsKey             = "external_cluster_details"
	cephFsStorageClassName                = "cephfs"
	cephRbdStorageClassName               = "ceph-rbd"
	cephRbdRadosNamespaceStorageClassName = "ceph-rbd-rados-namespace"
	cephRbdTopologyStorageClassName       = "ceph-rbd-topology"
	cephRgwStorageClassName               = "ceph-rgw"
	externalCephRgwEndpointKey            = "endpoint"
	cephRgwTLSSecretKey                   = "ceph-rgw-tls-cert"
)

const (
	rookCephOperatorConfigName = "rook-ceph-operator-config"
	rookEnableCephFSCSIKey     = "ROOK_CSI_ENABLE_CEPHFS"
)

// store the name of the rados-namespace
var radosNamespaceName string

var (
	// externalOCSResources will hold the ExternalResources for storageclusters
	// ExternalResources can be accessible using the UID of an storagecluster
	externalOCSResources = map[types.UID][]ExternalResource{}
)

// ExternalResource contains a list of External Cluster Resources
type ExternalResource struct {
	Kind string            `json:"kind"`
	Data map[string]string `json:"data"`
	Name string            `json:"name"`
}

type ocsExternalResources struct{}

// setRookCSICephFS function enables or disables the 'ROOK_CSI_ENABLE_CEPHFS' key
func (r *StorageClusterReconciler) setRookCSICephFS(
	enableDisableFlag bool, instance *ocsv1.StorageCluster) error {
	rookCephOperatorConfig := &corev1.ConfigMap{}
	err := r.Client.Get(context.TODO(),
		types.NamespacedName{Name: rookCephOperatorConfigName, Namespace: instance.ObjectMeta.Namespace},
		rookCephOperatorConfig)
	if err != nil {
		r.Log.Error(err, "Unable to get RookCeph ConfigMap.", "RookCephConfigMap", klog.KRef(instance.Namespace, rookCephOperatorConfigName))
		return err
	}
	enableDisableFlagStr := fmt.Sprintf("%v", enableDisableFlag)
	if rookCephOperatorConfig.Data == nil {
		rookCephOperatorConfig.Data = map[string]string{}
	}
	// if the current state of 'ROOK_CSI_ENABLE_CEPHFS' flag is same, just return
	if rookCephOperatorConfig.Data[rookEnableCephFSCSIKey] == enableDisableFlagStr {
		return nil
	}
	rookCephOperatorConfig.Data[rookEnableCephFSCSIKey] = enableDisableFlagStr
	return r.Client.Update(context.TODO(), rookCephOperatorConfig)
}

func checkEndpointReachable(endpoint string, timeout time.Duration) error {
	rxp := regexp.MustCompile(`^http[s]?://`)
	// remove any http or https protocols from the endpoint string
	endpoint = rxp.ReplaceAllString(endpoint, "")
	con, err := net.DialTimeout("tcp", endpoint, timeout)
	if err != nil {
		return err
	}
	defer con.Close()
	return nil
}

func sha512sum(tobeHashed []byte) (string, error) {
	h := sha512.New()
	if _, err := h.Write(tobeHashed); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func parseMonitoringIPs(monIP string) []string {
	return strings.Fields(strings.ReplaceAll(monIP, ",", " "))
}

// findNamedResourceFromArray retrieves the 'ExternalResource' with provided 'name'
func findNamedResourceFromArray(extArr []ExternalResource, name string) (ExternalResource, error) {
	for _, extR := range extArr {
		if extR.Name == name {
			return extR, nil
		}
	}
	return ExternalResource{}, fmt.Errorf("Unable to retrieve %q external resource", name)
}

func (r *StorageClusterReconciler) externalSecretDataChecksum(instance *ocsv1.StorageCluster) (string, error) {
	found, err := r.retrieveSecret(externalClusterDetailsSecret, instance)
	if err != nil {
		return "", err
	}
	return sha512sum(found.Data[externalClusterDetailsKey])
}

func (r *StorageClusterReconciler) sameExternalSecretData(instance *ocsv1.StorageCluster) bool {
	extSecretChecksum, err := r.externalSecretDataChecksum(instance)
	if err != nil {
		return false
	}
	// if the 'ExternalSecretHash' and fetched hash are same, then return true
	if instance.Status.ExternalSecretHash == extSecretChecksum {
		return true
	}
	// at this point the checksums are different, so update it
	instance.Status.ExternalSecretHash = extSecretChecksum
	return false
}

// retrieveSecret function retrieves the secret object with the specified name
func (r *StorageClusterReconciler) retrieveSecret(secretName string, instance *ocsv1.StorageCluster) (*corev1.Secret, error) {
	found := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: instance.Namespace,
		},
	}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: found.Name, Namespace: found.Namespace}, found)
	return found, err
}

// deleteSecret function delete the secret object with the specified name
func (r *StorageClusterReconciler) deleteSecret(instance *ocsv1.StorageCluster) error {
	found, err := r.retrieveSecret(externalClusterDetailsSecret, instance)
	if errors.IsNotFound(err) {
		r.Log.Info("External rhcs mode secret already deleted.")
		return nil
	}
	if err != nil {
		r.Log.Error(err, "Error while retrieving external rhcs mode secret.")
		return err
	}
	return r.Client.Delete(context.TODO(), found)
}

// retrieveExternalSecretData function retrieves the external secret and returns the data it contains
func (r *StorageClusterReconciler) retrieveExternalSecretData(
	instance *ocsv1.StorageCluster) ([]ExternalResource, error) {
	found, err := r.retrieveSecret(externalClusterDetailsSecret, instance)
	if err != nil {
		r.Log.Error(err, "Could not find the RookCeph external secret resource.")
		return nil, err
	}
	var data []ExternalResource
	err = json.Unmarshal(found.Data[externalClusterDetailsKey], &data)
	if err != nil {
		r.Log.Error(err, "Could not parse json blob.")
		return nil, err
	}
	return data, nil
}

func newExternalGatewaySpec(rgwEndpoint string, reqLogger logr.Logger, tlsEnabled bool) (*cephv1.GatewaySpec, error) {
	var gateWay cephv1.GatewaySpec
	hostIP, portStr, err := net.SplitHostPort(rgwEndpoint)
	if err != nil {
		reqLogger.Error(err,
			fmt.Sprintf("invalid rgw endpoint provided: %s", rgwEndpoint))
		return nil, err
	}
	if hostIP == "" {
		err := fmt.Errorf("An empty rgw host 'IP' address found")
		reqLogger.Error(err, "Host IP should not be empty in rgw endpoint")
		return nil, err
	}
	gateWay.ExternalRgwEndpoints = []cephv1.EndpointAddress{{IP: hostIP}}
	if net.ParseIP(hostIP) == nil {
		gateWay.ExternalRgwEndpoints = []cephv1.EndpointAddress{{Hostname: hostIP}}
	}
	var portInt64 int64
	if portInt64, err = strconv.ParseInt(portStr, 10, 32); err != nil {
		reqLogger.Error(err,
			fmt.Sprintf("invalid rgw 'port' provided: %s", portStr))
		return nil, err
	}
	if tlsEnabled {
		gateWay.SSLCertificateRef = cephRgwTLSSecretKey
		gateWay.SecurePort = int32(portInt64)
	} else {
		gateWay.Port = int32(portInt64)
	}
	// set PriorityClassName for the rgw pods
	gateWay.PriorityClassName = openshiftUserCritical
	gateWay.Instances = 1

	return &gateWay, nil
}

// newExternalCephObjectStoreInstances returns a set of CephObjectStores
// needed for external cluster mode
func (r *StorageClusterReconciler) newExternalCephObjectStoreInstances(
	initData *ocsv1.StorageCluster, rgwEndpoint string) ([]*cephv1.CephObjectStore, error) {
	// check whether the provided rgw endpoint is empty
	if rgwEndpoint = strings.TrimSpace(rgwEndpoint); rgwEndpoint == "" {
		r.Log.Info("Empty RGW Endpoint specified, external CephObjectStore won't be created.")
		return nil, nil
	}
	var tlsEnabled = false
	_, err := r.retrieveSecret(cephRgwTLSSecretKey, initData)
	// if we could retrieve a TLS secret, then enable TLS
	if err == nil {
		tlsEnabled = true
	}
	gatewaySpec, err := newExternalGatewaySpec(rgwEndpoint, r.Log, tlsEnabled)
	if err != nil {
		return nil, err
	}
	retObj := &cephv1.CephObjectStore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateNameForCephObjectStore(initData),
			Namespace: initData.Namespace,
		},
		Spec: cephv1.ObjectStoreSpec{
			Gateway: *gatewaySpec,
		},
	}
	retArrObj := []*cephv1.CephObjectStore{
		retObj,
	}
	return retArrObj, nil
}

// ensureCreated ensures that requested resources for the external cluster
// being created
func (obj *ocsExternalResources) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {

	// rhcs external mode
	data, err := r.retrieveExternalSecretData(instance)
	if err != nil {
		r.Log.Error(err, "Failed to retrieve external secret resources.")
		return reconcile.Result{}, err
	}
	externalOCSResources[instance.UID] = data

	if r.sameExternalSecretData(instance) {
		return reconcile.Result{}, nil
	}

	err = r.createExternalStorageClusterResources(instance)
	if err != nil {
		r.Log.Error(err, "Could not create ExternalStorageClusterResource.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

// ensureDeleted is dummy func for the ocsExternalResources
func (obj *ocsExternalResources) ensureDeleted(_ *StorageClusterReconciler, _ *ocsv1.StorageCluster) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

// createExternalStorageClusterResources creates external cluster resources
func (r *StorageClusterReconciler) createExternalStorageClusterResources(instance *ocsv1.StorageCluster) error {

	var err error
	var rgwEndpoint string

	ownerRef := metav1.OwnerReference{
		UID:        instance.UID,
		APIVersion: instance.APIVersion,
		Kind:       instance.Kind,
		Name:       instance.Name,
	}
	// this flag sets the 'ROOK_CSI_ENABLE_CEPHFS' flag
	enableRookCSICephFS := false
	// this stores only the StorageClasses specified in the Secret
	availableSCCs := []StorageClassConfiguration{}

	data, ok := externalOCSResources[instance.UID]
	if !ok {
		return fmt.Errorf("Unable to retrieve external resource from externalOCSResources")
	}

	var extCephObjectStores []*cephv1.CephObjectStore
	for _, d := range data {
		objectMeta := metav1.ObjectMeta{
			Name:            d.Name,
			Namespace:       instance.Namespace,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		}
		objectKey := types.NamespacedName{Name: d.Name, Namespace: instance.Namespace}
		switch d.Kind {
		case "CephCluster":
			// nothing to be done here,
			// as all the validation will be done in CephCluster creation
			if d.Name == "monitoring-endpoint" {
				continue
			}
		case "ConfigMap":
			cm := &corev1.ConfigMap{
				ObjectMeta: objectMeta,
				Data:       d.Data,
			}
			found := &corev1.ConfigMap{ObjectMeta: objectMeta}
			err := r.createExternalStorageClusterConfigMap(cm, found, objectKey)
			if err != nil {
				r.Log.Error(err, "Could not create ExternalStorageClusterConfigMap.", "ConfigMap", klog.KRef(cm.Namespace, cm.Name))
				return err
			}
		case "Secret":
			sec := &corev1.Secret{
				ObjectMeta: objectMeta,
				Data:       make(map[string][]byte),
			}
			for k, v := range d.Data {
				sec.Data[k] = []byte(v)
			}
			found := &corev1.Secret{ObjectMeta: objectMeta}
			err := r.createExternalStorageClusterSecret(sec, found, objectKey)
			if err != nil {
				r.Log.Error(err, "Could not create ExternalStorageClusterSecret.", "Secret", klog.KRef(sec.Namespace, sec.Name))
				return err
			}
		case "CephFilesystemSubVolumeGroup":
			found := &cephv1.CephFilesystemSubVolumeGroup{ObjectMeta: objectMeta}
			_, err := ctrl.CreateOrUpdate(context.TODO(), r.Client, found, func() error {
				found.Spec = cephv1.CephFilesystemSubVolumeGroupSpec{
					FilesystemName: d.Data["filesystemName"],
				}
				return nil
			})
			if err != nil {
				r.Log.Error(err, "Could not create CephFilesystemSubVolumeGroup.", "CephFilesystemSubVolumeGroup", klog.KRef(found.Namespace, found.Name))
				return err
			}
		case "CephBlockPoolRadosNamespace":
			radosNamespaceName = d.Data["radosNamespaceName"]
			rbdPool := d.Data["pool"]
			objectMeta.Name = radosNamespaceName
			radosNamespace := &cephv1.CephBlockPoolRadosNamespace{ObjectMeta: objectMeta}
			mutateFn := func() error {
				radosNamespace.Spec = cephv1.CephBlockPoolRadosNamespaceSpec{
					BlockPoolName: rbdPool,
				}
				return nil
			}
			_, err := ctrl.CreateOrUpdate(context.TODO(), r.Client, radosNamespace, mutateFn)
			if err != nil {
				r.Log.Error(err, "Could not create CephBlockPoolRadosNamespace.", "CephBlockPoolRadosNamespace", klog.KRef(radosNamespace.Namespace, radosNamespace.Name))
				return err
			}

		case "StorageClass":
			var scc StorageClassConfiguration
			var err error
			if d.Name == cephFsStorageClassName {
				scc = newCephFilesystemStorageClassConfiguration(instance)
				enableRookCSICephFS = true
			} else if d.Name == cephRbdStorageClassName {
				scc = newCephBlockPoolStorageClassConfiguration(instance)
			} else if d.Name == cephRbdRadosNamespaceStorageClassName {
				scc = newCephBlockPoolStorageClassConfiguration(instance)
				// update the storageclass name to rados storagesclass name
				scc.storageClass.Name = fmt.Sprintf("%s-%s", instance.Name, d.Name)
			} else if d.Name == cephRbdTopologyStorageClassName {
				scc = newNonResilientCephBlockPoolStorageClassConfiguration(instance)
				scc.storageClass.Parameters["topologyConstrainedPools"], err = getTopologyConstrainedPoolsExternalMode(d.Data)
				if err != nil {
					r.Log.Error(err, "Failed to get topologyConstrainedPools from external mode secret.", "StorageClass", klog.KRef(instance.Namespace, d.Name))
					return err
				}
			} else if d.Name == cephRgwStorageClassName {
				rgwEndpoint = d.Data[externalCephRgwEndpointKey]
				// rgw-endpoint is no longer needed in the 'd.Data' dictionary,
				// and can be deleted
				// created an issue in rook to add `CephObjectStore` type directly in the JSON output
				// https://github.com/rook/rook/issues/6165
				delete(d.Data, externalCephRgwEndpointKey)

				// do not create the rgw storageclass if the endpoint is not reachable
				err := checkEndpointReachable(rgwEndpoint, 5*time.Second)
				if err != nil {
					continue
				}
				scc = newCephOBCStorageClassConfiguration(instance)
			}

			if scc.storageClass == nil {
				continue
			}

			// now sc is pointing to appropriate StorageClass,
			// whose parameters have to be updated
			for k, v := range d.Data {
				scc.storageClass.Parameters[k] = v
			}
			availableSCCs = append(availableSCCs, scc)
		}
	}
	// creating only the available storageClasses
	err = r.createStorageClasses(availableSCCs, instance.Namespace)
	if err != nil {
		r.Log.Error(err, "Failed to create needed StorageClasses.")
		return err
	}

	if err = r.setRookCSICephFS(enableRookCSICephFS, instance); err != nil {
		r.Log.Error(err, "Failed to set RookEnableCephFSCSIKey to EnableRookCSICephFS.", "RookEnableCephFSCSIKey", rookEnableCephFSCSIKey, "EnableRookCSICephFS", enableRookCSICephFS)
		return err
	}

	if rgwEndpoint != "" {
		if err := checkEndpointReachable(rgwEndpoint, 5*time.Second); err != nil {
			r.Log.Error(err, "RGW endpoint is not reachable.", "RGWEndpoint", rgwEndpoint)
			return err
		}

		extCephObjectStores, err = r.newExternalCephObjectStoreInstances(instance, rgwEndpoint)
		if err != nil {
			return err
		}
		if extCephObjectStores != nil {
			if err = r.createCephObjectStores(extCephObjectStores, instance); err != nil {
				return err
			}
		}
	}
	return nil
}

func verifyMonitoringEndpoints(monitoringIP, monitoringPort string,
	log logr.Logger) (err error) {
	if monitoringIP == "" {
		err = fmt.Errorf(
			"Monitoring Endpoint not present in the external cluster secret %s",
			externalClusterDetailsSecret)
		log.Error(err, "Failed to get Monitoring IP.")
		return
	}
	if monitoringPort != "" {
		// replace any comma in the monitoring ip string with space
		// and then collect individual (non-empty) items' array
		monIPArr := parseMonitoringIPs(monitoringIP)
		for _, eachMonIP := range monIPArr {
			err = checkEndpointReachable(net.JoinHostPort(eachMonIP, monitoringPort), 5*time.Second)
			// if any one of the mon's IP:PORT combination is reachable,
			// consider the whole set as valid
			if err == nil {
				break
			}
		}
		if err != nil {
			log.Error(err, "Monitoring validation failed")
			return
		}
	}
	return
}

// createExternalStorageClusterConfigMap creates configmap for external cluster
func (r *StorageClusterReconciler) createExternalStorageClusterConfigMap(cm *corev1.ConfigMap, found *corev1.ConfigMap, objectKey types.NamespacedName) error {
	err := r.Client.Get(context.TODO(), objectKey, found)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Creating External StorageCluster ConfigMap.", "ConfigMap", klog.KRef(objectKey.Namespace, cm.Name))
			err = r.Client.Create(context.TODO(), cm)
			if err != nil {
				r.Log.Error(err, "Creation of External StorageCluster ConfigMap failed.", "ConfigMap", klog.KRef(objectKey.Namespace, cm.Name))
			}
		} else {
			r.Log.Error(err, "Unable the get the External StorageCluster ConfigMap.", "ConfigMap", klog.KRef(objectKey.Namespace, cm.Name))
		}
		return err
	}
	// update the found ConfigMap's Data with the latest changes,
	// if they don't match
	if !reflect.DeepEqual(found.Data, cm.Data) {
		found.Data = cm.DeepCopy().Data
		if err = r.Client.Update(context.TODO(), found); err != nil {
			return err
		}
	}
	return nil
}

// createExternalStorageClusterSecret creates secret for external cluster
func (r *StorageClusterReconciler) createExternalStorageClusterSecret(sec *corev1.Secret, found *corev1.Secret, objectKey types.NamespacedName) error {
	err := r.Client.Get(context.TODO(), objectKey, found)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Creating External StorageCluster Secret.", "Secret", klog.KRef(objectKey.Namespace, sec.Name))
			err = r.Client.Create(context.TODO(), sec)
			if err != nil {
				r.Log.Error(err, "Creation of External StorageCluster Secret failed.", "Secret", klog.KRef(objectKey.Namespace, sec.Name))
			}
		} else {
			r.Log.Error(err, "Unable the get External StorageCluster Secret", "Secret", klog.KRef(objectKey.Namespace, sec.Name))
		}
		return err
	}
	// update the found secret's Data with the latest changes,
	// if they don't match
	if !reflect.DeepEqual(found.Data, sec.Data) {
		found.Data = sec.DeepCopy().Data
		if err = r.Client.Update(context.TODO(), found); err != nil {
			return err
		}
	}
	return nil
}

func (r *StorageClusterReconciler) deleteExternalSecret(sc *ocsv1.StorageCluster) (err error) {
	// if 'externalStorage' is not enabled nothing to delete
	if !sc.Spec.ExternalStorage.Enable {
		return nil
	}
	err = r.deleteSecret(sc)
	if err != nil {
		r.Log.Error(err, "Error while deleting external rhcs mode secret.")
	}
	return err
}
