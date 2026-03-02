package ocsinitialization

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/platform"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/storagecluster"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/templates"

	"github.com/go-logr/logr"
	secv1client "github.com/openshift/client-go/security/clientset/versioned/typed/security/v1"
	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"open-cluster-management.io/api/cluster/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// operatorNamespace is the namespace the operator is running in
var operatorNamespace string

const (
	PrometheusOperatorDeploymentName = "prometheus-operator"
	PrometheusOperatorCSVNamePrefix  = "odf-prometheus-operator"
	ClusterClaimCrdName              = "clusterclaims.cluster.open-cluster-management.io"
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
	ctx context.Context

	Log               logr.Logger
	Scheme            *runtime.Scheme
	SecurityClient    secv1client.SecurityV1Interface
	OperatorNamespace string

	cache            cache.Cache
	controller       controller.Controller
	crdsBeingWatched sync.Map
}

// +kubebuilder:rbac:groups=ocs.openshift.io,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=get;create;update
// +kubebuilder:rbac:groups=security.openshift.io,resourceNames=privileged,resources=securitycontextconstraints,verbs=get;create;update
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources={alertmanagers,prometheuses},verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=servicemonitors,verbs=get;list;watch;update;patch;create;delete
// +kubebuilder:rbac:groups=operators.coreos.com,resources=clusterserviceversions,verbs=get;list;watch;delete;update;patch
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=clusterclaims,verbs=get;list;watch;create;update

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
		r.Log.Info(
			"Ignoring this resource. Only one OCSInitialization should exist.",
			"Expected",
			initNamespacedName,
			"Got",
			request.NamespacedName,
		)
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

	if err := r.reconcileDynamicWatches(); err != nil {
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

	if val, _ := r.crdsBeingWatched.Load(ClusterClaimCrdName); val.(bool) {
		err = r.ensureClusterClaimExists()
		if err != nil {
			r.Log.Error(err, "Failed to ensure odf-info namespacedname ClusterClaim")
			return reconcile.Result{}, err
		}
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

	if isROSAHCP, err := platform.IsPlatformROSAHCP(); err != nil {
		r.Log.Error(err, "Failed to determine if ROSA HCP cluster")
		return reconcile.Result{}, err
	} else if isROSAHCP {
		r.Log.Info("Setting up monitoring resources for ROSA HCP platform")
		err = r.reconcilePrometheusOperatorCSV(instance)
		if err != nil {
			r.Log.Error(err, "Failed to ensure prometheus operator deployment")
			return reconcile.Result{}, err
		}

		err = r.reconcilePrometheusKubeRBACConfigMap(instance)
		if err != nil {
			r.Log.Error(err, "Failed to ensure kubeRBACConfig config map")
			return reconcile.Result{}, err
		}

		err = r.reconcilePrometheusService(instance)
		if err != nil {
			r.Log.Error(err, "Failed to ensure prometheus service")
			return reconcile.Result{}, err
		}

		err = r.reconcilePrometheus(instance)
		if err != nil {
			r.Log.Error(err, "Failed to ensure prometheus instance")
			return reconcile.Result{}, err
		}

		err = r.reconcileAlertManager(instance)
		if err != nil {
			r.Log.Error(err, "Failed to ensure alertmanager instance")
			return reconcile.Result{}, err
		}

		err = r.reconcileK8sMetricsServiceMonitor(instance)
		if err != nil {
			r.Log.Error(err, "Failed to ensure k8sMetricsService Monitor")
			return reconcile.Result{}, err
		}
	}

	reason := ocsv1.ReconcileCompleted
	message := ocsv1.ReconcileCompletedMessage
	util.SetCompleteCondition(&instance.Status.Conditions, reason, message)

	instance.Status.Phase = util.PhaseReady
	err = r.Client.Status().Update(ctx, instance)

	return reconcile.Result{}, err
}

func (r *OCSInitializationReconciler) reconcileDynamicWatches() error {
	if watchExists, foundCrd := r.crdsBeingWatched.Load(ClusterClaimCrdName); !foundCrd || watchExists.(bool) {
		return nil
	}

	crd := &metav1.PartialObjectMetadata{}
	crd.SetGroupVersionKind(extv1.SchemeGroupVersion.WithKind("CustomResourceDefinition"))
	crd.Name = ClusterClaimCrdName
	if err := r.Get(r.ctx, client.ObjectKeyFromObject(crd), crd); client.IgnoreNotFound(err) != nil {
		return err
	}
	// CRD doesn't exist in the cluster
	if crd.UID == "" {
		return nil
	}

	// establish a watch
	if err := r.controller.Watch(
		source.Kind(
			r.cache,
			client.Object(&v1alpha1.ClusterClaim{}),
			handler.EnqueueRequestsFromMapFunc(
				func(context context.Context, obj client.Object) []reconcile.Request {
					return []reconcile.Request{{
						NamespacedName: InitNamespacedName(),
					}}
				},
			),
			util.NamePredicate(util.OdfInfoNamespacedNameClaimName),
			predicate.GenerationChangedPredicate{},
		),
	); err != nil {
		return fmt.Errorf("failed to setup dynamic watch on %s: %v", crd.Name, err)
	}

	r.crdsBeingWatched.Store(ClusterClaimCrdName, true)
	return nil
}

// SetupWithManager sets up a controller with a manager
func (r *OCSInitializationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	operatorNamespace = r.OperatorNamespace
	prometheusPredicate := predicate.NewPredicateFuncs(
		func(client client.Object) bool {
			return strings.HasPrefix(client.GetName(), PrometheusOperatorCSVNamePrefix)
		},
	)

	enqueueOCSInit := handler.EnqueueRequestsFromMapFunc(
		func(context context.Context, obj client.Object) []reconcile.Request {
			return []reconcile.Request{{
				NamespacedName: InitNamespacedName(),
			}}
		},
	)

	controller, err := ctrl.NewControllerManagedBy(mgr).
		For(&ocsv1.OCSInitialization{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Owns(&promv1.Prometheus{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&promv1.Alertmanager{}).
		Owns(&promv1.ServiceMonitor{}).
		// Watcher for storagecluster required to update
		// ocs-operator-config configmap if storagecluster is created or deleted
		Watches(
			&ocsv1.StorageCluster{},
			enqueueOCSInit,
			builder.WithPredicates(util.EventTypePredicate(true, false, true, false)),
		).
		// Watcher for rook-ceph-operator-config cm
		Watches(
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      util.RookCephOperatorConfigName,
					Namespace: r.OperatorNamespace,
				},
			},
			enqueueOCSInit,
		).
		// Watcher for ocs-operator-config cm
		Watches(
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      util.OcsOperatorConfigName,
					Namespace: r.OperatorNamespace,
				},
			},
			enqueueOCSInit,
		).
		// Watcher for prometheus operator csv
		Watches(
			&opv1a1.ClusterServiceVersion{},
			enqueueOCSInit,
			builder.WithPredicates(prometheusPredicate),
		).
		Watches(
			&extv1.CustomResourceDefinition{},
			enqueueOCSInit,
			builder.WithPredicates(
				predicate.NewPredicateFuncs(func(obj client.Object) bool {
					_, ok := r.crdsBeingWatched.Load(obj.GetName())
					return ok
				}),
				util.EventTypePredicate(true, false, false, false),
			),
			builder.OnlyMetadata,
		).
		Build(r)

	r.controller = controller
	r.cache = mgr.GetCache()
	r.crdsBeingWatched.Store(ClusterClaimCrdName, false)

	return err
}

func (r *OCSInitializationReconciler) ensureClusterClaimExists() error {
	operatorNamespace, err := util.GetOperatorNamespace()
	if err != nil {
		r.Log.Error(err, "failed to get operator's namespace. retrying again")
		return err
	}

	OdfInfoNamespacedName := types.NamespacedName{
		Namespace: operatorNamespace,
		Name:      storagecluster.OdfInfoConfigMapName,
	}.String()

	cc := &v1alpha1.ClusterClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: util.OdfInfoNamespacedNameClaimName,
		},
	}

	_, err = controllerutil.CreateOrUpdate(r.ctx, r.Client, cc, func() error {
		cc.Spec.Value = OdfInfoNamespacedName
		return nil
	})
	if err != nil {
		r.Log.Error(err, "failed to create or update clusterclaim", "ClusterClaim", util.OdfInfoNamespacedNameClaimName)
		return err
	}
	r.Log.Info("Created or updated clusterclaim", "ClusterClaim", cc.Name)

	return err
}

// ensureRookCephOperatorConfigExists ensures that the rook-ceph-operator-config cm exists
// This configmap is purely reserved for any user overrides to be applied.
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
	err := r.Client.Create(r.ctx, rookCephOperatorConfig)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// ensureOcsOperatorConfigExists ensures that the ocs-operator-config exists & if not create/update it with required values
// This configmap is reserved just for ocs operator use, primarily meant for passing values to rook-ceph-operator
// It is not meant to be modified by the user
// The values are set considering all storageclusters into account.
// The needed keys from the configmap are passed to rook-ceph operator pod as env variables.
// When any value in the configmap is updated, the rook-ceph-operator pod is restarted to pick up the new values.
func (r *OCSInitializationReconciler) ensureOcsOperatorConfigExists(initialData *ocsv1.OCSInitialization) error {

	storageClusterMetadataList := &metav1.PartialObjectMetadataList{}
	storageClusterMetadataList.SetGroupVersionKind(ocsv1.GroupVersion.WithKind("StorageCluster"))
	if err := r.List(r.ctx, storageClusterMetadataList, client.Limit(2)); err != nil {
		return err
	}

	ocsOperatorConfigData := map[string]string{
		util.RookCurrentNamespaceOnlyKey: strconv.FormatBool(!(len(storageClusterMetadataList.Items) > 1)),
		util.DisableCSIDriverKey:         strconv.FormatBool(true),
	}

	ocsOperatorConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.OcsOperatorConfigName,
			Namespace: initialData.Namespace,
		},
	}
	opResult, err := ctrl.CreateOrUpdate(r.ctx, r.Client, ocsOperatorConfig, func() error {

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

func (r *OCSInitializationReconciler) reconcilePrometheusKubeRBACConfigMap(initialData *ocsv1.OCSInitialization) error {
	prometheusKubeRBACConfigMap := &corev1.ConfigMap{}
	prometheusKubeRBACConfigMap.Name = templates.PrometheusKubeRBACProxyConfigMapName
	prometheusKubeRBACConfigMap.Namespace = initialData.Namespace

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, prometheusKubeRBACConfigMap, func() error {
		if err := ctrl.SetControllerReference(initialData, prometheusKubeRBACConfigMap, r.Scheme); err != nil {
			return err
		}
		prometheusKubeRBACConfigMap.Data = templates.KubeRBACProxyConfigMap.Data
		return nil
	})

	if err != nil {
		r.Log.Error(err, "Failed to create/update prometheus kube-rbac-proxy config map")
		return err
	}
	r.Log.Info("Prometheus kube-rbac-proxy config map creation succeeded", "Name", prometheusKubeRBACConfigMap.Name)
	return nil
}

func (r *OCSInitializationReconciler) reconcilePrometheusService(initialData *ocsv1.OCSInitialization) error {
	prometheusService := &corev1.Service{}
	prometheusService.Name = "prometheus"
	prometheusService.Namespace = initialData.Namespace

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, prometheusService, func() error {
		if err := ctrl.SetControllerReference(initialData, prometheusService, r.Scheme); err != nil {
			return err
		}
		util.AddAnnotation(
			prometheusService,
			"service.beta.openshift.io/serving-cert-secret-name",
			"prometheus-serving-cert-secret",
		)
		util.AddLabel(prometheusService, "prometheus", "odf-prometheus")
		prometheusService.Spec.Selector = map[string]string{
			"app.kubernetes.io/name": prometheusService.Name,
		}
		prometheusService.Spec.Ports = []corev1.ServicePort{
			{
				Name:       "https",
				Protocol:   corev1.ProtocolTCP,
				Port:       int32(templates.KubeRBACProxyPortNumber),
				TargetPort: intstr.FromString("https"),
			},
		}
		return nil
	})
	if err != nil {
		r.Log.Error(err, "Failed to create/update prometheus service")
		return err
	}
	r.Log.Info("Service creation succeeded", "Name", prometheusService.Name)
	return nil
}

func (r *OCSInitializationReconciler) reconcilePrometheus(initialData *ocsv1.OCSInitialization) error {
	prometheus := &promv1.Prometheus{}
	prometheus.Name = "odf-prometheus"
	prometheus.Namespace = initialData.Namespace

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, prometheus, func() error {
		if err := ctrl.SetControllerReference(initialData, prometheus, r.Scheme); err != nil {
			return err
		}
		templates.PrometheusSpecTemplate.DeepCopyInto(&prometheus.Spec)
		alertManagerEndpoint := util.Find(
			prometheus.Spec.Alerting.Alertmanagers,
			func(candidate *promv1.AlertmanagerEndpoints) bool {
				return candidate.Name == templates.AlertManagerEndpointName
			},
		)
		if alertManagerEndpoint == nil {
			return fmt.Errorf("unable to find AlertManagerEndpoint")
		}
		alertManagerEndpoint.Namespace = &initialData.Namespace
		return nil
	})

	if err != nil {
		r.Log.Error(err, "Failed to create/update prometheus instance")
		return err
	}
	r.Log.Info("Prometheus instance creation succeeded", "Name", prometheus.Name)

	return nil
}

func (r *OCSInitializationReconciler) reconcileAlertManager(initialData *ocsv1.OCSInitialization) error {
	alertManager := &promv1.Alertmanager{}
	alertManager.Name = "odf-alertmanager"
	alertManager.Namespace = initialData.Namespace

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, alertManager, func() error {
		if err := ctrl.SetControllerReference(initialData, alertManager, r.Scheme); err != nil {
			return err
		}
		util.AddAnnotation(alertManager, "prometheus", "odf-prometheus")
		templates.AlertmanagerSpecTemplate.DeepCopyInto(&alertManager.Spec)
		return nil
	})
	if err != nil {
		r.Log.Error(err, "Failed to create/update alertManager instance")
		return err
	}
	r.Log.Info("AlertManager instance creation succeeded", "Name", alertManager.Name)
	return nil
}

func (r *OCSInitializationReconciler) reconcileK8sMetricsServiceMonitor(initialData *ocsv1.OCSInitialization) error {
	k8sMetricsServiceMonitor := &promv1.ServiceMonitor{}
	k8sMetricsServiceMonitor.Name = "k8s-metrics-service-monitor"
	k8sMetricsServiceMonitor.Namespace = initialData.Namespace

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, k8sMetricsServiceMonitor, func() error {
		if err := ctrl.SetControllerReference(initialData, k8sMetricsServiceMonitor, r.Scheme); err != nil {
			return err
		}
		util.AddLabel(k8sMetricsServiceMonitor, "app", "odf-prometheus")
		templates.K8sMetricsServiceMonitorSpecTemplate.DeepCopyInto(&k8sMetricsServiceMonitor.Spec)
		return nil
	})
	if err != nil {
		r.Log.Error(err, "Failed to create/update K8s Metrics Service Monitor")
		return err
	}
	r.Log.Info("K8s Metrics Service Monitor creation succeeded", "Name", k8sMetricsServiceMonitor.Name)
	return nil

}

func (r *OCSInitializationReconciler) reconcilePrometheusOperatorCSV(initialData *ocsv1.OCSInitialization) error {
	csvList := &opv1a1.ClusterServiceVersionList{}
	if err := r.Client.List(r.ctx, csvList, client.InNamespace(initialData.Namespace)); err != nil {
		return fmt.Errorf("failed to list csvs in namespace %s,%v", initialData.Namespace, err)
	}
	csv := util.Find(
		csvList.Items,
		func(csv *opv1a1.ClusterServiceVersion) bool {
			return strings.HasPrefix(csv.Name, PrometheusOperatorCSVNamePrefix)
		},
	)
	if csv == nil {
		return fmt.Errorf("prometheus csv does not exist in namespace :%s", initialData.Namespace)
	}
	deploymentSpec := util.Find(
		csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs,
		func(deploymentSpec *opv1a1.StrategyDeploymentSpec) bool {
			return deploymentSpec.Name == PrometheusOperatorDeploymentName
		},
	)
	if deploymentSpec == nil {
		return fmt.Errorf("unable to find prometheus operator deployment spec")
	}

	deploymentSpec.Spec.Template.Spec.Tolerations = []corev1.Toleration{{
		Key:      defaults.NodeTolerationKey,
		Operator: corev1.TolerationOpEqual,
		Value:    "true",
		Effect:   corev1.TaintEffectNoSchedule,
	}}

	currentDeploymentSpec := deploymentSpec.DeepCopy()
	deploymentSpec.Spec.Replicas = ptr.To(int32(1))
	if !reflect.DeepEqual(currentDeploymentSpec, deploymentSpec) {
		if err := r.Client.Update(r.ctx, csv); err != nil {
			r.Log.Error(err, "Failed to update Prometheus csv")
			return err
		}
	}
	return nil
}
