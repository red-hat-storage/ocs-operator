package storagecluster

import (
	"context"
	"fmt"
	"os"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	routev1 "github.com/openshift/api/route/v1"
	templatev1 "github.com/openshift/api/template/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	"github.com/operator-framework/operator-lib/conditions"
	ocsclientv1a1 "github.com/red-hat-storage/ocs-client-operator/api/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	log = ctrl.Log.WithName("controllers").WithName("StorageCluster")
)

func (r *StorageClusterReconciler) initializeImageVars() error {
	r.images.Ceph = os.Getenv("CEPH_IMAGE")
	r.images.NooBaaCore = os.Getenv("NOOBAA_CORE_IMAGE")
	r.images.NooBaaDB = os.Getenv("NOOBAA_DB_IMAGE")
	r.images.OCSMetricsExporter = os.Getenv("OCS_METRICS_EXPORTER_IMAGE")
	r.images.KubeRBACProxy = os.Getenv("KUBE_RBAC_PROXY_IMAGE")

	if r.images.Ceph == "" {
		err := fmt.Errorf("CEPH_IMAGE environment variable not found")
		r.Log.Error(err, "Missing CEPH_IMAGE environment variable for ocs initialization.")
		return err
	} else if r.images.NooBaaCore == "" {
		err := fmt.Errorf("NOOBAA_CORE_IMAGE environment variable not found")
		r.Log.Error(err, "Missing NOOBAA_CORE_IMAGE environment variable for ocs initialization.")
		return err
	} else if r.images.NooBaaDB == "" {
		err := fmt.Errorf("NOOBAA_DB_IMAGE environment variable not found")
		r.Log.Error(err, "Missing NOOBAA_DB_IMAGE environment variable for ocs initialization.")
		return err
	} else if r.images.OCSMetricsExporter == "" {
		err := fmt.Errorf("OCS_METRICS_EXPORTER_IMAGE environment variable not found")
		r.Log.Error(err, "Missing OCS_METRICS_EXPORTER_IMAGE environment variable for ocs initialization.")
		return err
	} else if r.images.KubeRBACProxy == "" {
		err := fmt.Errorf("KUBE_RBAC_PROXY_IMAGE environment variable not found")
		r.Log.Error(err, "Missing KUBE_RBAC_PROXY_IMAGE environment variable for ocs initialization.")
		return err
	}
	return nil
}

// ImageMap holds mapping information between component image name and the image url
type ImageMap struct {
	Ceph               string
	NooBaaCore         string
	NooBaaDB           string
	OCSMetricsExporter string
	KubeRBACProxy      string
}

// StorageClusterReconciler reconciles a StorageCluster object
// nolint:revive
type StorageClusterReconciler struct {
	client.Client
	ctx                       context.Context
	Log                       logr.Logger
	Scheme                    *runtime.Scheme
	conditions                []conditionsv1.Condition
	phase                     string
	nodeCount                 int
	images                    ImageMap
	recorder                  *util.EventReporter
	OperatorCondition         conditions.Condition
	IsNoobaaStandalone        bool
	IsMultipleStorageClusters bool
	clusters                  *util.Clusters
	OperatorNamespace         string
	AvailableCrds             map[string]bool
}

// SetupWithManager sets up a controller with manager
func (r *StorageClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := r.initializeImageVars(); err != nil {
		return err
	}

	r.recorder = util.NewEventReporter(mgr.GetEventRecorderFor("controller_storagecluster"))

	// Compose a predicate that is an OR of the specified predicates
	scPredicate := util.ComposePredicates(
		predicate.GenerationChangedPredicate{},
		util.MetadataChangedPredicate{},
	)

	pvcPredicate := predicate.Funcs{
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}

	storageConsumerStatusPredicate := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.ObjectOld == nil || e.ObjectNew == nil {
				return false
			}
			oldObj := e.ObjectOld.(*ocsv1alpha1.StorageConsumer)
			newObj := e.ObjectNew.(*ocsv1alpha1.StorageConsumer)
			return !reflect.DeepEqual(oldObj.Status.Client, newObj.Status.Client)
		},
	}

	enqueueStorageClusterRequest := handler.EnqueueRequestsFromMapFunc(
		func(context context.Context, obj client.Object) []reconcile.Request {
			// Get the StorageCluster objects
			scList := &ocsv1.StorageClusterList{}
			err := r.Client.List(context, scList, &client.ListOptions{Namespace: obj.GetNamespace()})
			if err != nil {
				r.Log.Error(err, "Unable to list StorageCluster objects")
				return []reconcile.Request{}
			}

			// Return name and namespace of the StorageClusters object
			request := []reconcile.Request{}
			for _, sc := range scList.Items {
				request = append(request, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: sc.Namespace,
						Name:      sc.Name,
					},
				})
			}

			return request
		},
	)

	noobaaIgnoreTimeUpdatePredicate := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.ObjectOld == nil || e.ObjectNew == nil {
				return false
			}
			oldObj := e.ObjectOld.(*nbv1.NooBaa)
			newObj := e.ObjectNew.(*nbv1.NooBaa)

			ignorePaths := func(path cmp.Path) bool {
				switch path.String() {
				case "ObjectMeta.ManagedFields",
					"ObjectMeta.ResourceVersion",
					"Status.Conditions.LastHeartbeatTime",
					"Status.Conditions.LastTransitionTime":
					return true
				}
				return false
			}
			diff := cmp.Diff(
				oldObj, newObj,
				cmp.FilterPath(ignorePaths, cmp.Ignore()),
			)

			return diff != ""
		},
	}

	cephClusterIgnoreTimeUpdatePredicate := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.ObjectOld == nil || e.ObjectNew == nil {
				return false
			}
			oldObj := e.ObjectOld.(*cephv1.CephCluster)
			newObj := e.ObjectNew.(*cephv1.CephCluster)

			ignorePaths := func(path cmp.Path) bool {
				switch path.String() {
				case "ObjectMeta.ManagedFields",
					"ObjectMeta.ResourceVersion",
					"Status.CephStatus.LastChecked",
					"Status.CephStatus.Capacity.LastUpdated",
					"Status.Conditions.LastHeartbeatTime",
					"Status.Conditions.LastTransitionTime":
					return true
				}
				return false
			}
			diff := cmp.Diff(
				oldObj, newObj,
				cmp.FilterPath(ignorePaths, cmp.Ignore()),
			)

			return diff != ""
		},
	}

	crdPredicate := predicate.Funcs{
		CreateFunc: func(e event.TypedCreateEvent[client.Object]) bool {
			crdAvailable, keyExist := r.AvailableCrds[e.Object.GetName()]
			if keyExist && !crdAvailable {
				r.Log.Info("CustomResourceDefinition %s was Created.", e.Object.GetName())
				return true
			}
			return false
		},
		DeleteFunc: func(e event.TypedDeleteEvent[client.Object]) bool {
			crdAvailable, keyExist := r.AvailableCrds[e.Object.GetName()]
			if keyExist && crdAvailable {
				r.Log.Info("CustomResourceDefinition %s was Deleted.", e.Object.GetName())
				return true
			}
			return false
		},
		UpdateFunc: func(e event.TypedUpdateEvent[client.Object]) bool {
			return false
		},
		GenericFunc: func(e event.TypedGenericEvent[client.Object]) bool {
			return false
		},
	}

	build := ctrl.NewControllerManagedBy(mgr).
		For(&ocsv1.StorageCluster{}, builder.WithPredicates(scPredicate)).
		Owns(&cephv1.CephCluster{}, builder.WithPredicates(cephClusterIgnoreTimeUpdatePredicate)).
		Owns(&cephv1.CephBlockPool{}).
		Owns(&cephv1.CephFilesystem{}).
		Owns(&cephv1.CephFilesystemSubVolumeGroup{}).
		Owns(&cephv1.CephNFS{}).
		Owns(&cephv1.CephObjectStore{}).
		Owns(&cephv1.CephObjectStoreUser{}).
		Owns(&cephv1.CephRBDMirror{}).
		Owns(&corev1.PersistentVolumeClaim{}, builder.WithPredicates(pvcPredicate)).
		Owns(&appsv1.Deployment{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&corev1.Service{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&corev1.ConfigMap{}, builder.MatchEveryOwner, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&corev1.Secret{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&routev1.Route{}).
		Owns(&templatev1.Template{}).
		Watches(&extv1.CustomResourceDefinition{}, enqueueStorageClusterRequest, builder.WithPredicates(crdPredicate)).
		Watches(&storagev1.StorageClass{}, enqueueStorageClusterRequest).
		Watches(&volumesnapshotv1.VolumeSnapshotClass{}, enqueueStorageClusterRequest).
		Watches(&ocsclientv1a1.StorageClient{}, enqueueStorageClusterRequest).
		Watches(&ocsv1.StorageProfile{}, enqueueStorageClusterRequest).
		Watches(
			&extv1.CustomResourceDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Name: "virtualmachines.kubevirt.io",
				},
			},
			enqueueStorageClusterRequest,
		).
		Watches(&ocsv1alpha1.StorageConsumer{}, enqueueStorageClusterRequest, builder.WithPredicates(storageConsumerStatusPredicate))

	if os.Getenv("SKIP_NOOBAA_CRD_WATCH") != "true" {
		build.Owns(&nbv1.NooBaa{}, builder.WithPredicates(noobaaIgnoreTimeUpdatePredicate))
	}

	return build.Complete(r)
}
