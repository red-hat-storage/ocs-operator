/*
Copyright 2020 Red Hat OpenShift Container Storage.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package storageclusterpeer

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/go-logr/logr"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	providerClient "github.com/red-hat-storage/ocs-operator/v4/services/provider/client"
)

const (
	rBDMirrorName         = "rbd-mirror"
	bootstrapSecretPrefix = "bootstrap-secret"
)

// StorageClusterPeerReconciler reconciles a StorageClusterPeer object
// nolint:revive
type StorageClusterPeerReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	log                      logr.Logger
	ctx                      context.Context
	storageClusterPeer       *v1.StorageClusterPeer
	storageCluster           *v1.StorageCluster
	remoteStorageClusterName types.NamespacedName
	cephBlockPoolList        *rookCephv1.CephBlockPoolList
	markedForDeletion        bool
}

// SetupWithManager sets up the controller with the Manager.
func (r *StorageClusterPeerReconciler) SetupWithManager(mgr ctrl.Manager) error {

	cephBlockPoolPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Check if the label is not present
			if _, exist := e.Object.GetLabels()[util.CephBlockPoolForbidMirroringLabel]; exist {
				return false
			}
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Check if the label is not present
			if _, exist := e.Object.GetLabels()[util.CephBlockPoolForbidMirroringLabel]; exist {
				return false
			}
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.ObjectOld == nil || e.ObjectNew == nil {
				return false
			}
			oldObj := e.ObjectOld.(*rookCephv1.CephBlockPool)
			newObj := e.ObjectNew.(*rookCephv1.CephBlockPool)

			// requeue only if changes are made for
			return !(reflect.DeepEqual(oldObj.Spec.Mirroring, newObj.Spec.Mirroring) ||
				reflect.DeepEqual(oldObj.GetLabels(), newObj.GetLabels()))
		},
		GenericFunc: func(e event.TypedGenericEvent[client.Object]) bool {
			return false
		},
	}

	secretPredicate := predicate.Funcs{
		CreateFunc: func(e event.TypedCreateEvent[client.Object]) bool {
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return strings.HasPrefix(e.Object.GetName(), bootstrapSecretPrefix)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			if !strings.HasPrefix(e.ObjectOld.GetName(), bootstrapSecretPrefix) {
				return false
			}
			oldObj := e.ObjectOld.(*corev1.Secret)
			newObj := e.ObjectNew.(*corev1.Secret)
			return !reflect.DeepEqual(oldObj.Data, newObj.Data)
		},
		GenericFunc: func(e event.TypedGenericEvent[client.Object]) bool {
			return false
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.StorageClusterPeer{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&rookCephv1.CephBlockPool{}, builder.WithPredicates(cephBlockPoolPredicate)).
		Owns(&corev1.Secret{}, builder.WithPredicates(secretPredicate)).
		Owns(&rookCephv1.CephRBDMirror{}).
		Complete(r)
}

//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusterpeers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusterpeers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusterpeers/finalizers,verbs=update
//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusters,verbs=get;watch
//+kubebuilder:rbac:groups=ceph.rook.io,resources=cephblockpools,verbs=list;update;watch
//+kubebuilder:rbac:groups=ceph.rook.io,resources=cephrbdmirrors,verbs=create;update;watch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=create;update;delete;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *StorageClusterPeerReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	var err error
	r.ctx = ctx
	r.log = log.FromContext(ctx, "StorageClient", request)
	r.log.Info("Reconciling StorageClusterPeer.")

	// Fetch the StorageClusterPeer instance
	r.storageClusterPeer = &v1.StorageClusterPeer{}
	r.storageClusterPeer.Name = request.Name
	r.storageClusterPeer.Namespace = request.Namespace

	if err = r.get(r.storageClusterPeer); err != nil {
		if errors.IsNotFound(err) {
			r.log.Info("StorageClusterPeer resource not found. Ignoring since object must be deleted.")
			return reconcile.Result{}, nil
		}
		r.log.Error(err, "Failed to get StorageClusterPeer.")
		return reconcile.Result{}, err
	}

	// Fetch local StorageCluster
	r.storageCluster = &v1.StorageCluster{}
	r.storageCluster.Name = r.storageClusterPeer.Spec.LocalCluster.Name.Name
	r.storageCluster.Namespace = r.storageClusterPeer.Namespace
	if err = r.get(r.storageCluster); err != nil {
		if errors.IsNotFound(err) {
			r.log.Info("StorageCluster resource not found. Stopping Reconcile since object must be deleted.")
			return reconcile.Result{}, nil
		}
		r.log.Error(err, "Failed to get StorageCluster.")
		return reconcile.Result{}, err
	}

	r.remoteStorageClusterName = types.NamespacedName{
		Name:      r.storageClusterPeer.Spec.RemoteCluster.StorageClusterName.Name,
		Namespace: r.storageClusterPeer.Spec.RemoteCluster.StorageClusterName.Namespace}

	result, reconcileErr := r.reconcilePhases()

	// Apply status changes to the StorageClient
	statusErr := r.Client.Status().Update(ctx, r.storageClusterPeer)
	if statusErr != nil {
		r.log.Error(statusErr, "Failed to update StorageClusterPeer status.")
	}
	if reconcileErr != nil {
		err = reconcileErr
	} else if statusErr != nil {
		err = statusErr
	}
	return result, err
}

func (r *StorageClusterPeerReconciler) reconcilePhases() (ctrl.Result, error) {
	ocsClient, err := r.newExternalClusterClient()
	if err != nil {
		return reconcile.Result{}, err
	}
	defer ocsClient.Close()

	r.markedForDeletion = !r.storageClusterPeer.GetDeletionTimestamp().IsZero()

	// ensure finalizer
	if !r.markedForDeletion {
		if controllerutil.AddFinalizer(r.storageClusterPeer, v1.StorageClusterPeerFinalizer) {
			r.log.Info("Finalizer not found for StorageClusterPeer. Adding finalizer.", "StorageClusterPeer", r.storageClusterPeer.Name)
			if err := r.update(r.storageClusterPeer); err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to update StorageClusterPeer: %v", err)
			}
		}
	}

	if res, err := r.reconcileRemoteCluster(ocsClient); err != nil {
		return res, err
	} else if !res.IsZero() {
		// result is not empty
		return res, nil
	}

	if res, err := r.reconcileBlockPools(ocsClient); err != nil {
		return res, err
	} else if !res.IsZero() {
		// result is not empty
		return res, nil
	}

	//TODO: update status as Deleting
	if r.markedForDeletion {
		if controllerutil.RemoveFinalizer(r.storageClusterPeer, v1.StorageClusterPeerFinalizer) {
			r.log.Info("removing finalizer from StorageClusterPeer.", "StorageClusterPeer", r.storageClusterPeer.Name)
			if err := r.update(r.storageClusterPeer); err != nil {
				r.log.Info("Failed to remove finalizer from StorageClusterPeer", "StorageClusterPeer", r.storageClusterPeer.Name)
				return reconcile.Result{}, fmt.Errorf("failed to remove finalizer from StorageClient: %v", err)
			}
		}
	}

	return reconcile.Result{}, nil
}

func (r *StorageClusterPeerReconciler) reconcileRemoteCluster(ocsClient *providerClient.OCSProviderClient) (reconcile.Result, error) {
	if r.markedForDeletion {
		r.storageClusterPeer.Status.RemoteState = v1.StorageClusterPeerRemoteStateOffboarding
		return r.offboardRemoteCluster(ocsClient)
	}

	if r.storageClusterPeer.Status.RemoteState == "" || r.storageClusterPeer.Status.RemoteState == v1.StorageClusterPeerRemoteStateInitializing {
		return r.onboardRemoteCluster(ocsClient)
	} else if r.storageClusterPeer.Status.RemoteState == v1.StorageClusterPeerRemoteStateOnboarding {
		return r.acknowledgeOnboarding(ocsClient)
	}
	return reconcile.Result{}, nil
}

// newExternalClusterClient returns the *providerClient.OCSProviderClient
func (r *StorageClusterPeerReconciler) newExternalClusterClient() (*providerClient.OCSProviderClient, error) {

	ocsProviderClient, err := providerClient.NewProviderClient(
		r.ctx, r.storageClusterPeer.Spec.RemoteCluster.ApiEndpoint, time.Second*10)
	if err != nil {
		return nil, fmt.Errorf("failed to create a new provider client: %v", err)
	}

	return ocsProviderClient, nil
}

// offboardRemoteCluster makes an API call to the external storage provider cluster for offboarding
func (r *StorageClusterPeerReconciler) offboardRemoteCluster(ocsClient *providerClient.OCSProviderClient) (reconcile.Result, error) {

	_, err := ocsClient.OffboardStorageClusterPeer(r.ctx, string(r.storageClusterPeer.UID), r.remoteStorageClusterName)
	if err != nil {
		r.log.Error(err, fmt.Sprintf("failed to offboard from RemoteCluster, code: %v.", status.Code(err)))
		return reconcile.Result{}, fmt.Errorf("failed to offboard: %v", err)
	}
	r.storageClusterPeer.Status.RemoteState = v1.StorageClusterPeerRemoteStateOffboarded
	r.log.Info("StorageClusterPeer is offboarded", "StorageClusterPeer", r.storageClusterPeer.Name)
	return reconcile.Result{}, nil
}

// onboardRemoteCluster makes an API call to the external storage provider cluster for onboarding
func (r *StorageClusterPeerReconciler) onboardRemoteCluster(ocsClient *providerClient.OCSProviderClient) (reconcile.Result, error) {

	_, err := ocsClient.OnboardStorageClusterPeer(
		r.ctx,
		r.storageClusterPeer.Spec.RemoteCluster.OnboardingTicket,
		string(r.storageClusterPeer.UID),
		r.remoteStorageClusterName)
	if err != nil {
		r.log.Error(err, fmt.Sprintf("failed to Onboard to RemoteCluster, code: %v.", status.Code(err)))
		return reconcile.Result{}, fmt.Errorf("failed to onboard to RemoteCluster: %v", err)
	}

	r.storageClusterPeer.Status.RemoteState = v1.StorageClusterPeerRemoteStateOnboarding

	r.log.Info("onboarding started")
	return reconcile.Result{Requeue: true}, nil
}

// acknowledgeOnboarding makes an API call to the external storage provider cluster for onboarding
func (r *StorageClusterPeerReconciler) acknowledgeOnboarding(ocsClient *providerClient.OCSProviderClient) (reconcile.Result, error) {
	_, err := ocsClient.AcknowledgeOnboardingStorageClusterPeer(r.ctx, string(r.storageClusterPeer.UID), r.remoteStorageClusterName)
	if err != nil {
		r.log.Error(err, fmt.Sprintf("failed to Acknowledge Onboarding to RemoteCluster, code: %v.", status.Code(err)))
		return reconcile.Result{}, fmt.Errorf("failed to acknowledge onboarding: %v", err)
	}
	r.storageClusterPeer.Status.RemoteState = v1.StorageClusterPeerRemoteStateConnected

	r.log.Info("Onboarding is acknowledged successfully.")
	return reconcile.Result{Requeue: true}, nil
}

func (r *StorageClusterPeerReconciler) reconcileBlockPools(ocsClient *providerClient.OCSProviderClient) (reconcile.Result, error) {
	r.storageClusterPeer.Status.BlockPoolMirroringState = v1.BlockPoolMirroringStateInitializing
	selector := labels.NewSelector()
	var err error

	if r.storageClusterPeer.Spec.BlockPoolMirroring != nil {
		selector, err = metav1.LabelSelectorAsSelector(&r.storageClusterPeer.Spec.BlockPoolMirroring.Selector)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("error converting LabelSelector: %v", err)
		}
	}

	//Add Blacklist requirement
	blackListRequirement, err := labels.NewRequirement(util.CephBlockPoolForbidMirroringLabel, selection.DoesNotExist, []string{})
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("error creating blacklist Requirement for cephBlockPool: %v", err)
	}
	selector = selector.Add(*blackListRequirement)

	//list the blockPools without the labels - blacklist
	r.cephBlockPoolList = &rookCephv1.CephBlockPoolList{}
	if err := r.list(r.cephBlockPoolList,
		client.InNamespace(r.storageClusterPeer.Namespace),
		client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return reconcile.Result{}, err
	}

	if res, err := r.reconcileBlockPoolMirroring(); err != nil {
		return res, err
	}

	if res, err := r.getMirroringInfo(ocsClient); err != nil {
		return res, err
	}

	if res, err := r.reconcileBootstrapSecretForBlockPools(); err != nil {
		return res, err
	}

	if res, err := r.reconcileRBDMirrorDaemon(); err != nil {
		return res, err
	}

	if r.storageClusterPeer.Spec.BlockPoolMirroring == nil || r.markedForDeletion {
		r.storageClusterPeer.Status.BlockPoolMirroringState = v1.BlockPoolMirroringStateDisabled
	} else {
		r.storageClusterPeer.Status.BlockPoolMirroringState = v1.BlockPoolMirroringStateReady
	}

	return reconcile.Result{}, nil
}

func (r *StorageClusterPeerReconciler) reconcileBlockPoolMirroring() (reconcile.Result, error) {
	r.storageClusterPeer.Status.BlockPoolMirroringState = v1.BlockPoolMirroringStateEnabling
	storageClusterPeerList := &v1.StorageClusterPeerList{}
	if err := r.list(storageClusterPeerList, client.InNamespace(r.storageClusterPeer.Namespace)); err != nil {
		return reconcile.Result{}, err
	}

	for i := 0; i < len(r.cephBlockPoolList.Items); i++ {
		cephBlockPool := r.cephBlockPoolList.Items[i]

		enableMirroring := slices.ContainsFunc(storageClusterPeerList.Items, func(storageClusterPeer v1.StorageClusterPeer) bool {
			if storageClusterPeer.GetDeletionTimestamp().IsZero() && storageClusterPeer.Spec.BlockPoolMirroring != nil {
				return true
			}
			return false
		})

		// Condition to enable: if any one of the storageClusterPeer has this field enabled, enable mirroring
		if enableMirroring {
			cephBlockPool.Spec.Mirroring.Enabled = true
			cephBlockPool.Spec.Mirroring.Mode = "image"
		} else {
			cephBlockPool.Spec.Mirroring.Enabled = false
			cephBlockPool.Spec.Mirroring.Mode = ""
		}

		err := r.update(&cephBlockPool)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to update mirroring for CephBlockPool %q: %v", cephBlockPool.Name, err)
		}
	}
	return reconcile.Result{}, nil
}

func (r *StorageClusterPeerReconciler) getMirroringInfo(ocsClient *providerClient.OCSProviderClient) (reconcile.Result, error) {
	if r.storageClusterPeer.Spec.BlockPoolMirroring == nil || r.markedForDeletion {
		return reconcile.Result{}, nil
	}
	r.storageClusterPeer.Status.BlockPoolMirroringState = v1.BlockPoolMirroringStateFetchingBootstrapSecret

	var blockPoolNames []string
	for i := 0; i < len(r.cephBlockPoolList.Items); i++ {
		blockPoolNames = append(blockPoolNames, r.cephBlockPoolList.Items[i].Name)
	}

	config, err := ocsClient.GetMirroringInfo(r.ctx, string(r.storageClusterPeer.UID), blockPoolNames, r.remoteStorageClusterName)
	if err != nil {
		r.log.Error(err, fmt.Sprintf("failed to get Mirroring Info from RemoteCluster, code: %v.", status.Code(err)))
		return reconcile.Result{}, fmt.Errorf("failed to get mirroring info: %v", err)
	}

	for _, eResource := range config.ExternalResource {
		if eResource.Kind == "Secret" {
			data := map[string][]byte{}
			err = json.Unmarshal(eResource.Data, &data)
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to unmarshal StorageClusterPeer configuration response: %v", err)
			}
			secret := &corev1.Secret{}
			secret.Name = generateSecretName(eResource.Name, r.storageClusterPeer.Spec.RemoteCluster.ApiEndpoint, r.remoteStorageClusterName.String())
			secret.Namespace = r.storageClusterPeer.Namespace

			_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, secret, func() error {
				secret.Data = data
				return r.own(secret)
			})
			if err != nil {
				r.log.Error(err, "Failed to create/update the Bootstrap Secret for CephBlockPool", "CephBlockPool", eResource.Name, "Secret", secret.Name)
				return reconcile.Result{}, err
			}
		}
	}

	return reconcile.Result{}, nil
}

func (r *StorageClusterPeerReconciler) reconcileBootstrapSecretForBlockPools() (reconcile.Result, error) {
	r.storageClusterPeer.Status.BlockPoolMirroringState = v1.BlockPoolMirroringStateUpdatingBootstrapSecretRef

	for i := 0; i < len(r.cephBlockPoolList.Items); i++ {
		cephBlockPool := r.cephBlockPoolList.Items[i]
		secretName := generateSecretName(cephBlockPool.Name, r.storageClusterPeer.Spec.RemoteCluster.ApiEndpoint, r.remoteStorageClusterName.String())

		// set the secret ref
		if cephBlockPool.Spec.Mirroring.Peers == nil {
			cephBlockPool.Spec.Mirroring.Peers = &rookCephv1.MirroringPeerSpec{SecretNames: []string{}}
		}

		// if mirroring is disabled or it is marked for deletion
		if r.storageClusterPeer.Spec.BlockPoolMirroring == nil || r.markedForDeletion {
			index := slices.IndexFunc(cephBlockPool.Spec.Mirroring.Peers.SecretNames, func(s string) bool {
				return s == secretName
			})
			if index >= 0 {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: r.storageClusterPeer.Namespace},
				}
				err := r.delete(secret)
				if err != nil {
					return reconcile.Result{}, fmt.Errorf("failed to delete bootstrap secret: %v", err)
				}
				cephBlockPool.Spec.Mirroring.Peers.SecretNames = append(
					cephBlockPool.Spec.Mirroring.Peers.SecretNames[:index],
					cephBlockPool.Spec.Mirroring.Peers.SecretNames[index+1:]...)
			}
		} else {
			if !slices.Contains(cephBlockPool.Spec.Mirroring.Peers.SecretNames, secretName) {
				cephBlockPool.Spec.Mirroring.Peers.SecretNames = append(cephBlockPool.Spec.Mirroring.Peers.SecretNames, secretName)
			}
		}

		err := r.update(&cephBlockPool)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to update bootstrap secret ref on CephBlockPool %q: %v", cephBlockPool.Name, err)
		}
	}
	return reconcile.Result{}, nil
}

func (r *StorageClusterPeerReconciler) reconcileRBDMirrorDaemon() (reconcile.Result, error) {
	rbdMirror := &rookCephv1.CephRBDMirror{}
	rbdMirror.Name = rBDMirrorName
	rbdMirror.Namespace = r.storageClusterPeer.Namespace

	if r.storageClusterPeer.Spec.BlockPoolMirroring == nil || r.markedForDeletion {
		err := r.delete(rbdMirror)
		if err != nil {
			r.log.Error(err, "Failed to delete the CephRBDMirror", "CephRBDMirror", rbdMirror)
			return reconcile.Result{}, err
		}
	} else {
		_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, rbdMirror, func() error {
			rbdMirror.Spec.Count = 1
			return r.own(rbdMirror)
		})
		if err != nil {
			r.log.Error(err, "Failed to create/update the CephRBDMirror", "CephRBDMirror", rbdMirror)
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

func (r *StorageClusterPeerReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.ctx, key, obj)
}

func (r *StorageClusterPeerReconciler) list(obj client.ObjectList, listOptions ...client.ListOption) error {
	return r.Client.List(r.ctx, obj, listOptions...)
}

func (r *StorageClusterPeerReconciler) update(obj client.Object, opts ...client.UpdateOption) error {
	return r.Client.Update(r.ctx, obj, opts...)
}

func (r *StorageClusterPeerReconciler) delete(obj client.Object, opts ...client.DeleteOption) error {
	return r.Client.Delete(r.ctx, obj, opts...)
}

func (r *StorageClusterPeerReconciler) own(resource metav1.Object) error {
	// Ensure StorageClusterPeer ownership on a resource
	return controllerutil.SetControllerReference(r.storageClusterPeer, resource, r.Scheme)
}

func generateSecretName(poolName, apiServerEndpoint, remoteStorageCluster string) string {
	md5Sum := md5.Sum([]byte(fmt.Sprintf("%s-%s-%s", poolName, apiServerEndpoint, remoteStorageCluster)))
	return fmt.Sprintf("%s-%s", bootstrapSecretPrefix, hex.EncodeToString(md5Sum[:16]))
}
