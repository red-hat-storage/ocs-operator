package storagecluster

import (
	"context"
	"crypto/sha512"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	externalClusterDetailsSecret = "rook-ceph-external-cluster-details"
	externalClusterDetailsKey    = "external_cluster_details"
	cephFsStorageClassName       = "cephfs"
	cephRbdStorageClassName      = "ceph-rbd"
	cephRgwStorageClassName      = "ceph-rgw"
	externalCephRgwEndpointKey   = "endpoint"
)

const (
	rookCephOperatorConfigName = "rook-ceph-operator-config"
	rookEnableCephFSCSIKey     = "ROOK_CSI_ENABLE_CEPHFS"
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
		r.Log.Error(err, fmt.Sprintf("Unable to get '%s' config", rookCephOperatorConfigName))
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

// retrieveExternalSecretData function retrieves the external secret and returns the data it contains
func (r *StorageClusterReconciler) retrieveExternalSecretData(
	instance *ocsv1.StorageCluster) ([]ExternalResource, error) {
	found, err := r.retrieveSecret(externalClusterDetailsSecret, instance)
	if err != nil {
		r.Log.Error(err, "could not find the external secret resource")
		return nil, err
	}
	var data []ExternalResource
	err = json.Unmarshal(found.Data[externalClusterDetailsKey], &data)
	if err != nil {
		r.Log.Error(err, "could not parse json blob")
		return nil, err
	}
	return data, nil
}

func newExternalGatewaySpec(rgwEndpoint string, reqLogger logr.Logger) (*cephv1.GatewaySpec, error) {
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
	gateWay.ExternalRgwEndpoints = []corev1.EndpointAddress{{IP: hostIP}}
	var portInt64 int64
	if portInt64, err = strconv.ParseInt(portStr, 10, 32); err != nil {
		reqLogger.Error(err,
			fmt.Sprintf("invalid rgw 'port' provided: %s", portStr))
		return nil, err
	}
	gateWay.Port = int32(portInt64)
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
		r.Log.Info("WARNING: Empty RGW Endpoint specified, external CephObjectStore won't be created")
		return nil, nil
	}
	gatewaySpec, err := newExternalGatewaySpec(rgwEndpoint, r.Log)
	if err != nil {
		return nil, err
	}
	// enable bucket healthcheck
	healthCheck := cephv1.BucketHealthCheckSpec{
		Bucket: cephv1.HealthCheckSpec{
			Disabled: false,
			Interval: &metav1.Duration{Duration: time.Minute},
		},
	}
	retObj := &cephv1.CephObjectStore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateNameForCephObjectStore(initData),
			Namespace: initData.Namespace,
		},
		Spec: cephv1.ObjectStoreSpec{
			Gateway:     *gatewaySpec,
			HealthCheck: healthCheck,
		},
	}
	retArrObj := []*cephv1.CephObjectStore{
		retObj,
	}
	return retArrObj, nil
}

// ensureCreated ensures that requested resources for the external cluster
// being created
func (obj *ocsExternalResources) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {
	if r.sameExternalSecretData(instance) {
		return nil
	}
	err := r.createExternalStorageClusterResources(instance)
	if err != nil {
		r.Log.Error(err, "could not create ExternalStorageClusterResource")
		return err
	}
	return nil
}

// ensureDeleted is dummy func for the ocsExternalResources
func (obj *ocsExternalResources) ensureDeleted(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {
	return nil
}

// createExternalStorageClusterResources creates external cluster resources
func (r *StorageClusterReconciler) createExternalStorageClusterResources(instance *ocsv1.StorageCluster) error {
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
	data, err := r.retrieveExternalSecretData(instance)
	if err != nil {
		r.Log.Error(err, "failed to retrieve external resources")
		return err
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
			monitoringIP := d.Data["MonitoringEndpoint"]
			if monitoringIP == "" {
				err := fmt.Errorf(
					"Monitoring Endpoint not present in the external cluster secret %s",
					externalClusterDetailsSecret)
				r.Log.Error(err, "Failed to get Monitoring IP.")
				return err
			}
			// replace any comma in the monitoring ip string with space
			// and then collect individual (non-empty) items' array
			monIPArr := parseMonitoringIPs(monitoringIP)
			monitoringPort := d.Data["MonitoringPort"]
			if monitoringPort != "" {
				var err error
				for _, eachMonIP := range monIPArr {
					err = checkEndpointReachable(net.JoinHostPort(eachMonIP, monitoringPort), 5*time.Second)
					// if any one of the mon's IP:PORT combination is reachable,
					// consider the whole set as valid
					if err == nil {
						break
					}
				}
				if err != nil {
					r.Log.Error(err, "Monitoring validation failed")
					return err
				}
				r.monitoringPort = monitoringPort
			}
			r.Log.Info("Monitoring Information found. Monitoring will be enabled on the external cluster")
			r.monitoringIP = monitoringIP
		case "ConfigMap":
			cm := &corev1.ConfigMap{
				ObjectMeta: objectMeta,
				Data:       d.Data,
			}
			found := &corev1.ConfigMap{ObjectMeta: objectMeta}
			err := r.createExternalStorageClusterConfigMap(cm, found, objectKey)
			if err != nil {
				r.Log.Error(err, "could not create ExternalStorageClusterConfigMap")
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
				r.Log.Error(err, "could not create ExternalStorageClusterSecret")
				return err
			}
		case "StorageClass":
			var scc StorageClassConfiguration
			if d.Name == cephFsStorageClassName {
				scc = newCephFilesystemStorageClassConfiguration(instance)
				enableRookCSICephFS = true
			} else if d.Name == cephRbdStorageClassName {
				scc = newCephBlockPoolStorageClassConfiguration(instance)
			} else if d.Name == cephRgwStorageClassName {
				rgwEndpoint := d.Data[externalCephRgwEndpointKey]
				if err := checkEndpointReachable(rgwEndpoint, 5*time.Second); err != nil {
					r.Log.Error(err, fmt.Sprintf("RGW endpoint, %q, is not reachable", rgwEndpoint))
					return err
				}
				extCephObjectStores, err = r.newExternalCephObjectStoreInstances(instance, rgwEndpoint)
				if err != nil {
					return err
				}
				// rgw-endpoint is no longer needed in the 'd.Data' dictionary,
				// and can be deleted
				// created an issue in rook to add `CephObjectStore` type directly in the JSON output
				// https://github.com/rook/rook/issues/6165
				delete(d.Data, externalCephRgwEndpointKey)

				scc = newCephOBCStorageClassConfiguration(instance)
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
	err = r.createStorageClasses(availableSCCs)
	if err != nil {
		r.Log.Error(err, "failed to create needed StorageClasses")
		return err
	}
	if err = r.setRookCSICephFS(enableRookCSICephFS, instance); err != nil {
		r.Log.Error(err,
			fmt.Sprintf("failed to set '%s' to %v", rookEnableCephFSCSIKey, enableRookCSICephFS))
		return err
	}
	if extCephObjectStores != nil {
		if err = r.createCephObjectStores(extCephObjectStores, instance); err != nil {
			return err
		}
	}
	return nil
}

// createExternalStorageClusterConfigMap creates configmap for external cluster
func (r *StorageClusterReconciler) createExternalStorageClusterConfigMap(cm *corev1.ConfigMap, found *corev1.ConfigMap, objectKey types.NamespacedName) error {
	err := r.Client.Get(context.TODO(), objectKey, found)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info(fmt.Sprintf("creating configmap: %s", cm.Name))
			err = r.Client.Create(context.TODO(), cm)
			if err != nil {
				r.Log.Error(err, "creation of configmap failed")
				return err
			}
		} else {
			r.Log.Error(err, "unable the get the configmap")
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
			r.Log.Info(fmt.Sprintf("creating secret: %s", sec.Name))
			err = r.Client.Create(context.TODO(), sec)
			if err != nil {
				r.Log.Error(err, "creation of secret failed")
				return err
			}
		} else {
			r.Log.Error(err, "unable the get the secret")
			return err
		}
	}
	return nil
}
