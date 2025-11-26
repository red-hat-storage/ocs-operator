package storagecluster

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	"github.com/go-logr/logr"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	externalClusterDetailsSecret                = "rook-ceph-external-cluster-details"
	monEndpointConfigMapName                    = "rook-ceph-mon-endpoints"
	externalClusterDetailsKey                   = "external_cluster_details"
	cephFsStorageClassName                      = "cephfs"
	cephRbdStorageClassName                     = "ceph-rbd"
	cephRbdRadosNamespaceStorageClassNamePrefix = "ceph-rbd-rados-namespace"
	cephRbdTopologyStorageClassName             = "ceph-rbd-topology"
	cephRgwStorageClassName                     = "ceph-rgw"
	externalCephRgwEndpointKey                  = "endpoint"
	cephRgwTLSSecretKey                         = "ceph-rgw-tls-cert"
	storageClassSkippedError                    = "some storage classes were skipped while waiting for pre-requisites to be met"
	enableRbdDriverKey                          = "enableRbdDriver"
	enableCephfsDriverKey                       = "enableCephFsDriver"
	enableNfsDriverKey                          = "enableNfsDriver"
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

// StorageClassConfiguration provides configuration options for a StorageClass.
type StorageClassConfiguration struct {
	storageClass      *storagev1.StorageClass
	reconcileStrategy ReconcileStrategy
	isClusterExternal bool
}

type ocsExternalResources struct{}

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
	gateWay.PriorityClassName = systemClusterCritical
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
			Name:      util.GenerateNameForCephObjectStore(initData),
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

	err := r.createExternalStorageClusterResources(instance)
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

// setExternalOCSResourcesData retrieves data from the secret and stores it in the externalOCSResources global variable.
func (r *StorageClusterReconciler) setExternalOCSResourcesData(instance *ocsv1.StorageCluster) error {

	data, err := r.retrieveExternalSecretData(instance)
	if err != nil {
		r.Log.Error(err, "Failed to retrieve external secret resources.")
		return err
	}

	externalOCSResources[instance.UID] = data

	return nil
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
			scManagedResources := &instance.Spec.ManagedResources
			var scc StorageClassConfiguration
			if d.Name == cephFsStorageClassName {
				scc = StorageClassConfiguration{
					storageClass: util.NewDefaultCephFsStorageClass(
						instance.Namespace,
						util.GenerateNameForCephFilesystem(instance.Name),
						"rook-csi-cephfs-provisioner",
						"rook-csi-cephfs-node",
						instance.Namespace,
						"",
					),
					reconcileStrategy: ReconcileStrategy(scManagedResources.CephFilesystems.ReconcileStrategy),
					isClusterExternal: true,
				}
				scc.storageClass.Name = util.GenerateNameForCephFilesystemStorageClass(instance)
			} else if d.Name == cephRbdStorageClassName {
				scc = StorageClassConfiguration{
					storageClass: util.NewDefaultRbdStorageClass(
						instance.Namespace,
						util.GenerateNameForCephBlockPool(instance.Name),
						"rook-csi-rbd-provisioner",
						"rook-csi-rbd-node",
						instance.Namespace,
						"",
						"",
						scManagedResources.CephBlockPools.DefaultStorageClass,
					),
					reconcileStrategy: ReconcileStrategy(scManagedResources.CephBlockPools.ReconcileStrategy),
					isClusterExternal: true,
				}
				scc.storageClass.Name = util.GenerateNameForCephBlockPoolStorageClass(instance)
			} else if strings.HasPrefix(d.Name, cephRbdRadosNamespaceStorageClassNamePrefix) { // ceph-rbd-rados-namespace-<radosNamespaceName>
				scc = StorageClassConfiguration{
					storageClass: util.NewDefaultRbdStorageClass(
						instance.Namespace,
						util.GenerateNameForCephBlockPool(instance.Name),
						"rook-csi-rbd-provisioner",
						"rook-csi-rbd-node",
						instance.Namespace,
						"",
						"",
						scManagedResources.CephBlockPools.DefaultStorageClass,
					),
					reconcileStrategy: ReconcileStrategy(scManagedResources.CephBlockPools.ReconcileStrategy),
					isClusterExternal: true,
				}
				// update the storageclass name to rados storagesclass name
				scc.storageClass.Name = fmt.Sprintf("%s-%s", instance.Name, d.Name)
			} else if d.Name == cephRbdTopologyStorageClassName {
				topologyConstrainedPools, err := getTopologyConstrainedPoolsExternalMode(d.Data)
				if err != nil {
					r.Log.Error(
						err,
						"Failed to get topologyConstrainedPools from external mode secret.",
						"StorageClass",
						klog.KRef(instance.Namespace, d.Name),
					)
					return err
				}
				scc = StorageClassConfiguration{
					storageClass: util.NewDefaultNonResilientRbdStorageClass(
						instance.Namespace,
						topologyConstrainedPools,
						"rook-csi-rbd-provisioner",
						"rook-csi-rbd-node",
						instance.Namespace,
						"",
						"",
					),
					isClusterExternal: true,
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
				scc = StorageClassConfiguration{
					storageClass: util.NewDefaultOBCStorageClass(
						instance.Namespace,
						util.GenerateNameForCephObjectStore(instance),
					),
					reconcileStrategy: ReconcileStrategy(scManagedResources.CephObjectStores.ReconcileStrategy),
					isClusterExternal: true,
				}
				scc.storageClass.Name = util.GenerateNameForCephRgwStorageClass(instance)
			}

			if scc.storageClass == nil {
				continue
			}

			// now sc is pointing to appropriate StorageClass,
			// whose parameters have to be updated
			for k, v := range d.Data {
				scc.storageClass.Parameters[k] = v
			}
			// add external mode label to storageclass
			util.AddLabel(scc.storageClass, util.ExternalClassLabelKey, strconv.FormatBool(true))
			availableSCCs = append(availableSCCs, scc)
		}
	}

	err = r.configureCsiDrivers(availableSCCs, instance)
	if err != nil {
		r.Log.Error(err, "Failed to configure CSI drivers.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
		return err
	}

	// creating only the available storageClasses
	err = r.createExternalModeStorageClasses(availableSCCs, instance.Namespace)
	if err != nil {
		r.Log.Error(err, "Failed to create needed StorageClasses.")
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

func (r *StorageClusterReconciler) createExternalModeStorageClasses(sccs []StorageClassConfiguration, namespace string) error {
	var skippedSC []string
	for _, scc := range sccs {
		if scc.reconcileStrategy == ReconcileStrategyIgnore {
			continue
		}
		sc := scc.storageClass

		switch {
		case strings.Contains(sc.Name, "-rados-namespace"):
			// if rados namespace is provided, update the `storageclass cluster-id = rados-namespace cluster-id`
			if radosNamespaceName == "" {
				r.Log.Info("radosNamespaceName not updated successfully")
				skippedSC = append(skippedSC, sc.Name)
				continue
			}
			radosNamespace := cephv1.CephBlockPoolRadosNamespace{}
			key := types.NamespacedName{Name: radosNamespaceName, Namespace: namespace}
			err := r.Client.Get(context.TODO(), key, &radosNamespace)
			if err != nil || radosNamespace.Status == nil || radosNamespace.Status.Phase != cephv1.ConditionType(util.PhaseReady) || radosNamespace.Status.Info["clusterID"] == "" {
				r.Log.Info("Waiting for radosNamespace to be Ready. Skip reconciling StorageClass",
					"radosNamespace", klog.KRef(key.Namespace, key.Name),
					"StorageClass", klog.KRef("", sc.Name),
				)
				skippedSC = append(skippedSC, sc.Name)
				continue
			}
			sc.Parameters["clusterID"] = radosNamespace.Status.Info["clusterID"]
		case strings.Contains(sc.Name, "-nfs") || strings.Contains(sc.Provisioner, util.NfsDriverName):
			// wait for CephNFS to be ready
			cephNFS := cephv1.CephNFS{}
			key := types.NamespacedName{Name: sc.Parameters["nfsCluster"], Namespace: namespace}
			err := r.Client.Get(context.TODO(), key, &cephNFS)
			if err != nil || cephNFS.Status == nil || cephNFS.Status.Phase != util.PhaseReady {
				r.Log.Info("Waiting for CephNFS to be Ready. Skip reconciling StorageClass",
					"CephNFS", klog.KRef(key.Namespace, key.Name),
					"StorageClass", klog.KRef("", sc.Name),
				)
				skippedSC = append(skippedSC, sc.Name)
				continue
			}
		}

		scRecreated := false
		existing := &storagev1.StorageClass{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: sc.Name, Namespace: sc.Namespace}, existing)

		if errors.IsNotFound(err) {
			// Since the StorageClass is not found, we will create a new one
			r.Log.Info("Creating StorageClass.", "StorageClass", klog.KRef(sc.Namespace, existing.Name))
			err = r.Client.Create(context.TODO(), sc)
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if scc.reconcileStrategy == ReconcileStrategyInit {
				continue
			}
			if existing.DeletionTimestamp != nil {
				return fmt.Errorf("failed to restore StorageClass  %s because it is marked for deletion", existing.Name)
			}
			if !reflect.DeepEqual(sc.Parameters, existing.Parameters) || existing.Labels[util.ExternalClassLabelKey] != sc.Labels[util.ExternalClassLabelKey] {
				// Since we have to update the existing StorageClass
				// So, we will delete the existing storageclass and create a new one
				r.Log.Info("StorageClass needs to be updated, deleting it.", "StorageClass", klog.KRef(sc.Namespace, existing.Name))
				err = r.Client.Delete(context.TODO(), existing)
				if err != nil {
					r.Log.Error(err, "Failed to delete StorageClass.", "StorageClass", klog.KRef(sc.Namespace, existing.Name))
					return err
				}
				r.Log.Info("Creating StorageClass.", "StorageClass", klog.KRef(sc.Namespace, sc.Name))
				err = r.Client.Create(context.TODO(), sc)
				if err != nil {
					r.Log.Info("Failed to create StorageClass.", "StorageClass", klog.KRef(sc.Namespace, sc.Name))
					return err
				}
				scRecreated = true
			}
			if !scRecreated {
				// Delete existing key rotation annotation and set it on sc only when it is false
				delete(existing.Annotations, defaults.KeyRotationEnableAnnotation)
				if krState := sc.GetAnnotations()[defaults.KeyRotationEnableAnnotation]; krState == "false" {
					util.AddAnnotation(existing, defaults.KeyRotationEnableAnnotation, krState)
				}

				err = r.Client.Update(context.TODO(), existing)
				if err != nil {
					r.Log.Error(err, "Failed to update annotations on the StorageClass.", "StorageClass", klog.KRef(sc.Namespace, existing.Name))
					return err
				}
			}
		}
	}
	if len(skippedSC) > 0 {
		return fmt.Errorf("%s: [%s]", storageClassSkippedError, strings.Join(skippedSC, ","))
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
	if cm.ObjectMeta.Name != monEndpointConfigMapName && !reflect.DeepEqual(found.Data, cm.Data) {
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

// getTopologyConstrainedPoolsExternalMode constructs the topologyConstrainedPools string for external mode from the data map
func getTopologyConstrainedPoolsExternalMode(data map[string]string) (string, error) {
	type topologySegment struct {
		DomainLabel string `json:"domainLabel"`
		DomainValue string `json:"value"`
	}
	// TopologyConstrainedPool stores the pool name and a list of its associated topology domain values.
	type topologyConstrainedPool struct {
		PoolName       string            `json:"poolName"`
		DomainSegments []topologySegment `json:"domainSegments"`
	}
	var topologyConstrainedPools []topologyConstrainedPool

	domainLabel := data["topologyFailureDomainLabel"]
	domainValues := strings.Split(data["topologyFailureDomainValues"], ",")
	poolNames := strings.Split(data["topologyPools"], ",")

	// Check if the number of pool names and domain values are equal
	if len(poolNames) != len(domainValues) {
		return "", fmt.Errorf("number of pool names and domain values are not equal")
	}

	for i, poolName := range poolNames {
		topologyConstrainedPools = append(topologyConstrainedPools, topologyConstrainedPool{
			PoolName: poolName,
			DomainSegments: []topologySegment{
				{
					DomainLabel: domainLabel,
					DomainValue: domainValues[i],
				},
			},
		})
	}
	// returning as string as parameters are of type map[string]string
	topologyConstrainedPoolsStr, err := json.MarshalIndent(topologyConstrainedPools, "", "  ")
	if err != nil {
		return "", err
	}
	return string(topologyConstrainedPoolsStr), nil
}

// getTopologyFailureDomainConfig retrieves the topology failure domain label from external resources
func (r *StorageClusterReconciler) getTopologyFailureDomainConfig(uid types.UID) (string, error) {
	data, ok := externalOCSResources[uid]
	if !ok {
		return "", fmt.Errorf("unable to retrieve external resource from externalOCSResources")
	}

	// Look for the topologyFailureDomainLabel in the external resources
	for _, d := range data {
		if d.Kind == "StorageClass" && d.Name == cephRbdTopologyStorageClassName {
			label, ok := d.Data["TOPOLOGY_FAILURE_DOMAIN_LABEL"]
			if !ok {
				break
			}
			if len(label) == 0 {
				return "", fmt.Errorf("topology failure domain label value is empty")
			}
			fullLabel := util.GetFullTopologyLabel(label)
			if fullLabel == "" {
				return "", fmt.Errorf("invalid topology failure domain label: %s", label)
			}
			r.Log.Info("Found topology failure domain label from external resources", "label", fullLabel)
			return fullLabel, nil
		}
	}
	return "", nil
}

func (r *StorageClusterReconciler) configureCsiDrivers(availableSCCs []StorageClassConfiguration, instance *ocsv1.StorageCluster) error {
	clientConfig := &corev1.ConfigMap{}
	clientConfig.Name = ocsClientConfigMapName
	clientConfig.Namespace = r.OperatorNamespace

	if err := r.Client.Get(r.ctx, client.ObjectKeyFromObject(clientConfig), clientConfig); err != nil {
		r.Log.Error(err, "failed to get ocs client operator configmap", "configmap", klog.KRef(clientConfig.Namespace, clientConfig.Name))
		return err
	}

	existingData := maps.Clone(clientConfig.Data)
	if clientConfig.Data == nil {
		clientConfig.Data = map[string]string{}
	}

	for _, scc := range availableSCCs {
		switch scc.storageClass.Provisioner {
		case util.RbdDriverName:
			clientConfig.Data[enableRbdDriverKey] = strconv.FormatBool(true)
		case util.CephFSDriverName:
			clientConfig.Data[enableCephfsDriverKey] = strconv.FormatBool(true)
		case util.NfsDriverName:
			clientConfig.Data[enableNfsDriverKey] = strconv.FormatBool(true)
		default:
			r.Log.Info("not enabling driver for: %s", scc.storageClass.Provisioner)
		}

	}

	// Read topology-failure-domain-label from external resources and update ConfigMap
	topologyDomainLabel, err := r.getTopologyFailureDomainConfig(instance.UID)
	if err != nil {
		r.Log.Error(err, "failed to get topology failure domain config from external resources")
		return err
	}
	if topologyDomainLabel != "" {
		clientConfig.Data["topologyFailureDomainLabels"] = topologyDomainLabel
	}

	if !maps.Equal(clientConfig.Data, existingData) {
		if err := r.Client.Update(r.ctx, clientConfig); err != nil {
			r.Log.Error(err, "failed to update client operator's configmap data", "configmap", klog.KRef(clientConfig.Namespace, clientConfig.Name))
			return err
		}
	}
	return nil
}
