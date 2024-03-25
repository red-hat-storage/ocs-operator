package ocsinitialization

import (
	"context"
	"fmt"
	"reflect"
	"strconv"

	"github.com/go-logr/logr"
	secv1client "github.com/openshift/client-go/security/clientset/versioned/typed/security/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// operatorNamespace is the namespace the operator is running in
var operatorNamespace string

const (
	wrongNamespacedName     = "Ignoring this resource. Only one should exist, and this one has the wrong name and/or namespace."
	random30CharacterString = "KP7TThmSTZegSGmHuPKLnSaaAHSG3RSgqw6akBj0oVk"
)

// InitNamespacedName returns a NamespacedName for the singleton instance that
// should exist.
func InitNamespacedName() types.NamespacedName {
	return types.NamespacedName{
		Name:      "ocsinit",
		Namespace: operatorNamespace,
	}
}

// OCSInitializationReconciler reconciles a OCSInitialization object
// nolint:revive
type OCSInitializationReconciler struct {
	client.Client
	ctx               context.Context
	Log               logr.Logger
	Scheme            *runtime.Scheme
	SecurityClient    secv1client.SecurityV1Interface
	OperatorNamespace string
	clusters          *util.Clusters
}

// +kubebuilder:rbac:groups=ocs.openshift.io,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=get;create;update
// +kubebuilder:rbac:groups=security.openshift.io,resourceNames=privileged,resources=securitycontextconstraints,verbs=get;create;update

// Reconcile reads that state of the cluster for a OCSInitialization object and makes changes based on the state read
// and what is in the OCSInitialization.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *OCSInitializationReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {

	prevLogger := r.Log
	defer func() { r.Log = prevLogger }()
	r.Log = r.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	r.ctx = ctx

	r.Log.Info("Reconciling OCSInitialization.", "OCSInitialization", klog.KRef(request.Namespace, request.Name))

	initNamespacedName := InitNamespacedName()
	instance := &ocsv1.OCSInitialization{}
	if initNamespacedName.Name != request.Name || initNamespacedName.Namespace != request.Namespace {
		// Ignoring this resource because it has the wrong name or namespace
		r.Log.Info(wrongNamespacedName)
		err := r.Client.Get(ctx, request.NamespacedName, instance)
		if err != nil {
			// the resource probably got deleted
			if errors.IsNotFound(err) {
				return reconcile.Result{}, nil
			}
			return reconcile.Result{}, err
		}

		instance.Status.Phase = util.PhaseIgnored
		err = r.Client.Status().Update(ctx, instance)
		if err != nil {
			r.Log.Error(err, "Failed to update ignored OCSInitialization resource.", "OCSInitialization", klog.KRef(instance.Namespace, instance.Name))
		}
		return reconcile.Result{}, err
	}

	// Fetch the OCSInitialization instance
	err := r.Client.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Recreating since we depend on this to exist. A user may delete it to
			// induce a reset of all initial data.
			r.Log.Info("Recreating OCSInitialization resource.")
			return reconcile.Result{}, r.Client.Create(ctx, &ocsv1.OCSInitialization{
				ObjectMeta: metav1.ObjectMeta{
					Name:      initNamespacedName.Name,
					Namespace: initNamespacedName.Namespace,
				},
			})
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if instance.Status.Conditions == nil {
		reason := ocsv1.ReconcileInit
		message := "Initializing OCSInitialization resource"
		util.SetProgressingCondition(&instance.Status.Conditions, reason, message)

		instance.Status.Phase = util.PhaseProgressing
		err = r.Client.Status().Update(ctx, instance)
		if err != nil {
			r.Log.Error(err, "Failed to add conditions to status of OCSInitialization resource.", "OCSInitialization", klog.KRef(instance.Namespace, instance.Name))
			return reconcile.Result{}, err
		}
	}

	r.clusters, err = util.GetClusters(ctx, r.Client)
	if err != nil {
		r.Log.Error(err, "Failed to get clusters")
		return reconcile.Result{}, err
	}

	err = r.ensureSCCs(instance)
	if err != nil {
		reason := ocsv1.ReconcileFailed
		message := fmt.Sprintf("Error while reconciling: %v", err)
		util.SetErrorCondition(&instance.Status.Conditions, reason, message)

		instance.Status.Phase = util.PhaseError
		// don't want to overwrite the actual reconcile failure
		uErr := r.Client.Status().Update(ctx, instance)
		if uErr != nil {
			r.Log.Error(uErr, "Failed to update conditions of OCSInitialization resource.", "OCSInitialization", klog.KRef(instance.Namespace, instance.Name))
		}
		return reconcile.Result{}, err
	}
	instance.Status.SCCsCreated = true

	err = r.Client.Status().Update(ctx, instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.ensureRookCephOperatorConfigExists(instance)
	if err != nil {
		r.Log.Error(err, "Failed to ensure rook-ceph-operator-config ConfigMap")
		return reconcile.Result{}, err
	}

	err = r.ensureOcsOperatorConfigExists(instance)
	if err != nil {
		r.Log.Error(err, "Failed to ensure ocs-operator-config ConfigMap")
		return reconcile.Result{}, err
	}

	err = r.reconcileUXBackendSecret(instance)
	if err != nil {
		r.Log.Error(err, "Failed to ensure uxbackend secret")
		return reconcile.Result{}, err
	}

	err = r.reconcileUXBackendService(instance)
	if err != nil {
		r.Log.Error(err, "Failed to ensure uxbackend service")
		return reconcile.Result{}, err
	}

	reason := ocsv1.ReconcileCompleted
	message := ocsv1.ReconcileCompletedMessage
	util.SetCompleteCondition(&instance.Status.Conditions, reason, message)

	instance.Status.Phase = util.PhaseReady
	err = r.Client.Status().Update(ctx, instance)

	return reconcile.Result{}, err
}

// SetupWithManager sets up a controller with a manager
func (r *OCSInitializationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	operatorNamespace = r.OperatorNamespace

	return ctrl.NewControllerManagedBy(mgr).
		For(&ocsv1.OCSInitialization{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		// Watcher for storagecluster required to update
		// ocs-operator-config configmap if storagecluster spec changes
		Watches(
			&ocsv1.StorageCluster{},
			handler.EnqueueRequestsFromMapFunc(
				func(context context.Context, obj client.Object) []reconcile.Request {
					return []reconcile.Request{{
						NamespacedName: InitNamespacedName(),
					}}
				},
			),
		).
		// Watcher for storageClass required to update values related to replica-1
		// in ocs-operator-config configmap, if storageClass changes
		Watches(
			&storagev1.StorageClass{},
			handler.EnqueueRequestsFromMapFunc(
				func(context context.Context, obj client.Object) []reconcile.Request {
					// Only reconcile if the storageClass has topologyConstrainedPools set
					sc := obj.(*storagev1.StorageClass)
					if sc.Parameters["topologyConstrainedPools"] != "" {
						return []reconcile.Request{{
							NamespacedName: InitNamespacedName(),
						}}
					}
					return []reconcile.Request{}
				},
			),
		).
		// Watcher for rook-ceph-operator-config cm
		Watches(
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      util.RookCephOperatorConfigName,
					Namespace: r.OperatorNamespace,
				},
			},
			handler.EnqueueRequestsFromMapFunc(
				func(context context.Context, obj client.Object) []reconcile.Request {
					return []reconcile.Request{{
						NamespacedName: InitNamespacedName(),
					}}
				},
			),
		).
		// Watcher for ocs-operator-config cm
		Watches(
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      util.OcsOperatorConfigName,
					Namespace: r.OperatorNamespace,
				},
			},
			handler.EnqueueRequestsFromMapFunc(
				func(context context.Context, obj client.Object) []reconcile.Request {
					return []reconcile.Request{{
						NamespacedName: InitNamespacedName(),
					}}
				},
			),
		).
		Complete(r)
}

// ensureRookCephOperatorConfigExists ensures that the rook-ceph-operator-config cm exists
// This configmap is semi-reserved for any user overrides to be applied 4.16 onwards.
// Earlier it used to be purely reserved for user overrides.
// We don't reconcile it if it exists as it can reset any values the user has set
// The configmap is watched by the rook operator and values set here have higher precedence
// than the default values set in the rook operator pod env vars.
func (r *OCSInitializationReconciler) ensureRookCephOperatorConfigExists(initialData *ocsv1.OCSInitialization) error {
	rookCephOperatorConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.RookCephOperatorConfigName,
			Namespace: initialData.Namespace,
		},
	}

	opResult, err := ctrl.CreateOrUpdate(r.ctx, r.Client, rookCephOperatorConfig, func() error {

		if rookCephOperatorConfig.Data == nil {
			rookCephOperatorConfig.Data = make(map[string]string)
		}

		csiPluginDefaults := defaults.DaemonPlacements[defaults.CsiPluginKey]
		csiPluginTolerations := r.getCsiTolerations(defaults.CsiPluginKey)
		if err := updateTolerationsConfigFunc(rookCephOperatorConfig,
			csiPluginTolerations, csiPluginDefaults.Tolerations, "CSI_PLUGIN_TOLERATIONS",
			&initialData.Status.RookCephOperatorConfig.CsiPluginTolerationsModified); err != nil {
			return err
		}

		csiProvisionerDefaults := defaults.DaemonPlacements[defaults.CsiProvisionerKey]
		csiProvisionerTolerations := r.getCsiTolerations(defaults.CsiProvisionerKey)
		if err := updateTolerationsConfigFunc(rookCephOperatorConfig,
			csiProvisionerTolerations, csiProvisionerDefaults.Tolerations, "CSI_PROVISIONER_TOLERATIONS",
			&initialData.Status.RookCephOperatorConfig.CsiProvisionerTolerationsModified); err != nil {
			return err // nolint:revive
		}

		return nil
	})

	if err != nil {
		r.Log.Error(err, "Failed to create/update rook-ceph-operator-config configmap")
		return err
	}
	r.Log.Info("Successfully created/updated rook-ceph-operator-config configmap", "OperationResult", opResult)

	return nil
}

func updateTolerationsConfigFunc(rookCephOperatorConfig *corev1.ConfigMap,
	tolerations, defaults []corev1.Toleration, configMapKey string, modifiedFlag *bool) error {

	if tolerations != nil {
		updatedTolerations := append(tolerations, defaults...)
		tolerationsYAML, err := yaml.Marshal(updatedTolerations)
		if err != nil {
			return err
		}
		rookCephOperatorConfig.Data[configMapKey] = string(tolerationsYAML)
		*modifiedFlag = true
	} else if tolerations == nil && *modifiedFlag {
		delete(rookCephOperatorConfig.Data, configMapKey)
		*modifiedFlag = false
	}

	return nil
}

func (r *OCSInitializationReconciler) getCsiTolerations(csiTolerationKey string) []corev1.Toleration {

	var tolerations []corev1.Toleration

	clusters := r.clusters.GetStorageClusters()

	for i := range clusters {
		if val, ok := clusters[i].Spec.Placement[rookCephv1.KeyType(csiTolerationKey)]; ok {
			tolerations = append(tolerations, val.Tolerations...)
		}
	}

	return tolerations
}

// ensureOcsOperatorConfigExists ensures that the ocs-operator-config exists & if not create/update it with required values
// This configmap is reserved just for ocs operator use, primarily meant for passing values to rook-ceph-operator
// It is not meant to be modified by the user
// The values are set considering all storageclusters into account.
// The needed keys from the configmap are passed to rook-ceph operator pod as env variables.
// When any value in the configmap is updated, the rook-ceph-operator pod is restarted to pick up the new values.
func (r *OCSInitializationReconciler) ensureOcsOperatorConfigExists(initialData *ocsv1.OCSInitialization) error {
	ocsOperatorConfigData := map[string]string{
		util.ClusterNameKey:              util.GetClusterID(r.ctx, r.Client, &r.Log),
		util.RookCurrentNamespaceOnlyKey: strconv.FormatBool(!(len(r.clusters.GetStorageClusters()) > 1)),
		util.EnableTopologyKey:           r.getEnableTopologyKeyValue(),
		util.TopologyDomainLabelsKey:     r.getTopologyDomainLabelsKeyValue(),
		util.EnableNFSKey:                r.getEnableNFSKeyValue(),
		util.CsiEnableCephFSKey:          "true",
		util.CsiEnableRBDKey:             "true",
	}

	ocsOperatorConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.OcsOperatorConfigName,
			Namespace: initialData.Namespace,
		},
	}
	opResult, err := ctrl.CreateOrUpdate(r.ctx, r.Client, ocsOperatorConfig, func() error {

		// If the configmap is being created for the first time, set the entry for
		// CSI_REMOVE_HOLDER_PODS to "true". This configuration is applied for new clusters
		// starting from ODF version 4.16 onwards. For old or upgraded clusters,
		// it's initially set to "false", allowing users time to manually migrate from holder pods.
		// In ODF version 4.17, we will universally set it to "true" for all users.

		if ocsOperatorConfig.CreationTimestamp.IsZero() {
			ocsOperatorConfigData[util.CsiRemoveHolderPodsKey] = "true"
		} else if ocsOperatorConfig.Data[util.CsiRemoveHolderPodsKey] == "" {
			ocsOperatorConfigData[util.CsiRemoveHolderPodsKey] = "false"
		} else if ocsOperatorConfig.Data[util.CsiRemoveHolderPodsKey] != "" {
			ocsOperatorConfigData[util.CsiRemoveHolderPodsKey] = ocsOperatorConfig.Data[util.CsiRemoveHolderPodsKey]
		}

		if !reflect.DeepEqual(ocsOperatorConfig.Data, ocsOperatorConfigData) {
			r.Log.Info("Updating ocs-operator-config configmap")
			ocsOperatorConfig.Data = ocsOperatorConfigData
		}

		// This configmap was controlled by the storageCluster before 4.15.
		// We are required to remove storageCluster as a controller before adding OCSInitialization as controller.
		if existing := metav1.GetControllerOfNoCopy(ocsOperatorConfig); existing != nil && existing.Kind == "StorageCluster" {
			existing.BlockOwnerDeletion = nil
			existing.Controller = nil
		}

		return ctrl.SetControllerReference(initialData, ocsOperatorConfig, r.Scheme)
	})
	if err != nil {
		r.Log.Error(err, "Failed to create/update ocs-operator-config configmap", "OperationResult", opResult)
		return err
	}
	// If configmap is created or updated, restart the rook-ceph-operator pod to pick up the new change
	if opResult == controllerutil.OperationResultCreated || opResult == controllerutil.OperationResultUpdated {
		r.Log.Info("ocs-operator-config configmap created/updated. Restarting rook-ceph-operator pod to pick up the new values")
		util.RestartPod(r.ctx, r.Client, &r.Log, "rook-ceph-operator", initialData.Namespace)
	}

	return nil
}

func (r *OCSInitializationReconciler) getEnableTopologyKeyValue() string {

	for _, sc := range r.clusters.GetStorageClusters() {
		if !sc.Spec.ExternalStorage.Enable && sc.Spec.ManagedResources.CephNonResilientPools.Enable {
			// In internal mode return true even if one of the storageCluster has enabled it via the CR
			return "true"
		} else if sc.Spec.ExternalStorage.Enable {
			// In external mode, check if the non-resilient storageClass exists
			scName := util.GenerateNameForNonResilientCephBlockPoolSC(&sc)
			storageClass := util.GetStorageClassWithName(r.ctx, r.Client, scName)
			if storageClass != nil {
				return "true"
			}
		}
	}

	return "false"
}

// In case of multiple storageClusters when replica-1 is enabled for both an internal and an external cluster, different failure domain keys can lead to complications.
// To prevent this, when gathering information for the external cluster, ensure that the failure domain is specified to match that of the internal cluster (sc.Status.FailureDomain).
func (r *OCSInitializationReconciler) getTopologyDomainLabelsKeyValue() string {

	for _, sc := range r.clusters.GetStorageClusters() {
		if !sc.Spec.ExternalStorage.Enable && sc.Spec.ManagedResources.CephNonResilientPools.Enable {
			// In internal mode return the failure domain key directly from the storageCluster
			return sc.Status.FailureDomainKey
		} else if sc.Spec.ExternalStorage.Enable {
			// In external mode, check if the non-resilient storageClass exists
			// determine the failure domain key from the storageClass parameter
			scName := util.GenerateNameForNonResilientCephBlockPoolSC(&sc)
			storageClass := util.GetStorageClassWithName(r.ctx, r.Client, scName)
			if storageClass != nil {
				return getFailureDomainKeyFromStorageClassParameter(storageClass)
			}
		}
	}

	return ""
}

func (r *OCSInitializationReconciler) getEnableNFSKeyValue() string {

	// return true even if one of the storagecluster is using NFS
	for _, sc := range r.clusters.GetStorageClusters() {
		if sc.Spec.NFS != nil && sc.Spec.NFS.Enable {
			return "true"
		}
	}

	return "false"
}

func getFailureDomainKeyFromStorageClassParameter(sc *storagev1.StorageClass) string {
	failuredomain := sc.Parameters["topologyFailureDomainLabel"]
	if failuredomain == "zone" {
		return "topology.kubernetes.io/zone"
	} else if failuredomain == "rack" {
		return "topology.rook.io/rack"
	} else if failuredomain == "hostname" || failuredomain == "host" {
		return "kubernetes.io/hostname"
	} else {
		return ""
	}
}

func (r *OCSInitializationReconciler) reconcileUXBackendSecret(initialData *ocsv1.OCSInitialization) error {

	var err error

	secret := &corev1.Secret{}
	secret.Name = "ux-backend-proxy"
	secret.Namespace = initialData.Namespace

	_, err = ctrl.CreateOrUpdate(r.ctx, r.Client, secret, func() error {

		if err := ctrl.SetControllerReference(initialData, secret, r.Scheme); err != nil {
			return err
		}

		secret.StringData = map[string]string{
			"session_secret": random30CharacterString,
		}

		return nil
	})

	if err != nil {
		r.Log.Error(err, "Failed to create/update ux-backend secret")
		return err
	}

	r.Log.Info("Secret creation succeeded", "Name", secret.Name)

	return nil
}

func (r *OCSInitializationReconciler) reconcileUXBackendService(initialData *ocsv1.OCSInitialization) error {

	var err error

	service := &corev1.Service{}
	service.Name = "ux-backend-proxy"
	service.Namespace = initialData.Namespace

	_, err = ctrl.CreateOrUpdate(r.ctx, r.Client, service, func() error {

		if err := ctrl.SetControllerReference(initialData, service, r.Scheme); err != nil {
			return err
		}

		service.Annotations = map[string]string{
			"service.beta.openshift.io/serving-cert-secret-name": "ux-cert-secret",
		}
		service.Spec = corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "proxy",
					Port:     8888,
					Protocol: corev1.ProtocolTCP,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 8888,
					},
				},
			},
			Selector:        map[string]string{"app": "ux-backend-server"},
			SessionAffinity: "None",
			Type:            "ClusterIP",
		}

		return nil

	})

	if err != nil {
		r.Log.Error(err, "Failed to create/update ux-backend service")
		return err
	}
	r.Log.Info("Service creation succeeded", "Name", service.Name)

	return nil
}
