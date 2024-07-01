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
	"github.com/go-logr/logr"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	providerClient "github.com/red-hat-storage/ocs-operator/v4/services/provider/client"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	v1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
)

const (
	// grpcCallNames
	OnboardMirrorPeer               = "OnboardMirrorPeer"
	OffboardMirrorPeer              = "OffboardMirrorPeer"
	GetMirroringInfo                = "GetMirroringInfo"
	AcknowledgeMirrorPeerOnboarding = "AcknowledgeMirrorPeerOnboarding"

	rBDMirrorName         = "rbd-mirror"
	bootstrapSecretPrefix = "bootstrap-secret"
)

// StorageClusterPeerReconciler reconciles a StorageClusterPeer object
// nolint:revive
type StorageClusterPeerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger

	ctx                context.Context
	storageClusterPeer *v1.StorageClusterPeer
	cephBlockPoolList  []*rookCephv1.CephBlockPool
}

// SetupWithManager sets up the controller with the Manager.
func (r *StorageClusterPeerReconciler) SetupWithManager(mgr ctrl.Manager) error {

	cephBlockPoolPredicate := predicate.Funcs{
		CreateFunc: func(createEvent event.CreateEvent) bool {
			// Check if the annotation is not present
			if createEvent.Object.GetAnnotations() == nil {
				return false
			}
			if _, exists := createEvent.Object.GetAnnotations()[util.CephBlockPoolBlackListAnnotation]; !exists {
				return false
			}
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Check if the annotation is not present
			if e.Object.GetAnnotations() == nil {
				return false
			}
			if _, exists := e.Object.GetAnnotations()[util.CephBlockPoolBlackListAnnotation]; !exists {
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
				reflect.DeepEqual(oldObj.GetAnnotations(), newObj.GetAnnotations()))
		},
	}

	secretPredicate := predicate.Funcs{
		DeleteFunc: func(e event.DeleteEvent) bool {
			return strings.HasPrefix(e.Object.GetName(), bootstrapSecretPrefix)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return strings.HasPrefix(e.ObjectOld.GetName(), bootstrapSecretPrefix)
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
//+kubebuilder:rbac:groups=ceph.rook.io,resources=cephblockpools;,verbs=list;update;
//+kubebuilder:rbac:groups=ceph.rook.io,resources=cephrbdmirrors;,verbs=create;update
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=create;update;delete
//+kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *StorageClusterPeerReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	var err error
	r.ctx = ctrllog.IntoContext(ctx, r.Log)
	r.Log.Info("Reconciling StorageClusterPeer.")

	// Fetch the StorageClusterPeer instance
	r.storageClusterPeer = &v1.StorageClusterPeer{}
	r.storageClusterPeer.Name = request.Name
	r.storageClusterPeer.Namespace = request.Namespace

	if err = r.get(r.storageClusterPeer); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("StorageClusterPeer resource not found. Ignoring since object must be deleted.")
			return reconcile.Result{}, nil
		}
		r.Log.Error(err, "Failed to get StorageClusterPeer.")
		return reconcile.Result{}, err
	}

	// Don't Reconcile the StorageClusterPeer if it is in failed state
	if r.storageClusterPeer.Status.Phase == v1.StorageClusterPeerFailed {
		return reconcile.Result{}, nil
	}

	//list the blockPools without the annotation - blacklist
	cephBlockPoolList := &rookCephv1.CephBlockPoolList{}
	if err := r.list(cephBlockPoolList, client.InNamespace(r.storageClusterPeer.Namespace)); err != nil {
		return reconcile.Result{}, err
	}

	r.cephBlockPoolList = []*rookCephv1.CephBlockPool{}
	for _, cephBlockPool := range cephBlockPoolList.Items {
		if _, hasAnnotation := cephBlockPool.GetAnnotations()[util.CephBlockPoolBlackListAnnotation]; !hasAnnotation {
			r.cephBlockPoolList = append(r.cephBlockPoolList, &cephBlockPool)
		}
	}

	result, reconcileErr := r.reconcilePhases()

	// Apply status changes to the StorageClient
	statusErr := r.Client.Status().Update(ctx, r.storageClusterPeer)
	if statusErr != nil {
		r.Log.Error(statusErr, "Failed to update StorageClusterPeer status.")
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

	// deletion phase
	if !r.storageClusterPeer.GetDeletionTimestamp().IsZero() {
		return r.deletionPhase(ocsClient)
	}

	// ensure finalizer
	if controllerutil.AddFinalizer(r.storageClusterPeer, v1.StorageClusterPeerFinalizer) {
		r.storageClusterPeer.Status.Phase = v1.StorageClusterPeerInitializing
		r.Log.Info("Finalizer not found for StorageClusterPeer. Adding finalizer.", "StorageClusterPeer", r.storageClusterPeer.Name)
		if err := r.update(r.storageClusterPeer); err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to update StorageClusterPeer: %v", err)
		}
	}

	if r.storageClusterPeer.Status.PeerID == "" {
		return r.onboardProvider(ocsClient)
	} else if r.storageClusterPeer.Status.Phase == v1.StorageClusterPeerOnboarding {
		return r.acknowledgeOnboarding(ocsClient)
	}

	if res, err := r.reconcileRBDMirrorDaemon(); err != nil {
		return res, err
	}

	if res, err := r.getMirroringInfo(ocsClient); err != nil {
		return res, err
	}

	if res, err := r.reconcileBlockPools(); err != nil {
		return res, err
	}

	return reconcile.Result{}, nil
}

func (r *StorageClusterPeerReconciler) deletionPhase(ocsClient *providerClient.OCSProviderClient) (ctrl.Result, error) {

	r.storageClusterPeer.Status.Phase = v1.StorageClusterPeerOffboarding

	if res, err := r.deleteSecretRefOnBlockPools(); err != nil {
		return res, err
	}

	if res, err := r.offboardProvider(ocsClient); err != nil {
		r.Log.Error(err, "Offboarding in progress.")
		return res, err
	} else if !res.IsZero() {
		// result is not empty
		return res, nil
	}

	if controllerutil.RemoveFinalizer(r.storageClusterPeer, v1.StorageClusterPeerFinalizer) {
		r.Log.Info("removing finalizer from StorageClusterPeer.", "StorageClusterPeer", r.storageClusterPeer.Name)
		if err := r.update(r.storageClusterPeer); err != nil {
			r.Log.Info("Failed to remove finalizer from StorageClusterPeer", "StorageClusterPeer", r.storageClusterPeer.Name)
			return reconcile.Result{}, fmt.Errorf("failed to remove finalizer from StorageClient: %v", err)
		}
	}
	r.Log.Info("StorageClusterPeer is offboarded", "StorageClusterPeer", r.storageClusterPeer.Name)
	return reconcile.Result{}, nil
}

// newExternalClusterClient returns the *providerClient.OCSProviderClient
func (r *StorageClusterPeerReconciler) newExternalClusterClient() (*providerClient.OCSProviderClient, error) {

	ocsProviderClient, err := providerClient.NewProviderClient(
		r.ctx, r.storageClusterPeer.Spec.APIServerEndpoint, time.Second*10)
	if err != nil {
		return nil, fmt.Errorf("failed to create a new provider client: %v", err)
	}

	return ocsProviderClient, nil
}

// onboardProvider makes an API call to the external storage provider cluster for onboarding
func (r *StorageClusterPeerReconciler) onboardProvider(ocsClient *providerClient.OCSProviderClient) (reconcile.Result, error) {

	clusterVersion := &configv1.ClusterVersion{}
	clusterVersion.Name = "version"
	if err := r.get(clusterVersion); err != nil {
		r.Log.Error(err, "failed to get the clusterVersion version of the OCP cluster")
		return reconcile.Result{}, fmt.Errorf("failed to get the clusterVersion version of the OCP cluster: %v", err)
	}

	name := fmt.Sprintf("storageclusterpeer-%s", clusterVersion.Spec.ClusterID)

	response, err := ocsClient.OnboardMirroringPeer(r.ctx, r.storageClusterPeer.Spec.OnboardingTicket, name)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			r.logGrpcError(OnboardMirrorPeer, err, st.Code())
		}
		return reconcile.Result{}, fmt.Errorf("failed to onboard: %v", err)
	}

	if response.UUID == "" {
		err = fmt.Errorf("storage provider response is empty")
		r.Log.Error(err, "empty response")
		return reconcile.Result{}, err
	}

	r.storageClusterPeer.Status.PeerID = response.UUID
	r.storageClusterPeer.Status.Phase = v1.StorageClusterPeerOnboarding

	r.Log.Info("onboarding started")
	return reconcile.Result{Requeue: true}, nil
}

// acknowledgeOnboarding makes an API call to the external storage provider cluster for onboarding
func (r *StorageClusterPeerReconciler) acknowledgeOnboarding(ocsClient *providerClient.OCSProviderClient) (reconcile.Result, error) {
	_, err := ocsClient.AcknowledgeMirrorPeerOnboarding(r.ctx, r.storageClusterPeer.Status.PeerID)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			r.logGrpcError(AcknowledgeMirrorPeerOnboarding, err, st.Code())
		}
		r.Log.Error(err, "Failed to acknowledge onboarding.")
		return reconcile.Result{}, fmt.Errorf("failed to acknowledge onboarding: %v", err)
	}
	r.storageClusterPeer.Status.Phase = v1.StorageClusterPeerConnected

	r.Log.Info("Onboarding is acknowledged successfully.")
	return reconcile.Result{Requeue: true}, nil
}

func (r *StorageClusterPeerReconciler) offboardProvider(ocsClient *providerClient.OCSProviderClient) (reconcile.Result, error) {

	_, err := ocsClient.OffboardMirroringPeer(r.ctx, r.storageClusterPeer.Status.PeerID)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			r.logGrpcError(OffboardMirrorPeer, err, st.Code())
		}
		return reconcile.Result{}, fmt.Errorf("failed to offboard: %v", err)
	}

	return reconcile.Result{}, nil
}

func (r *StorageClusterPeerReconciler) getMirroringInfo(ocsClient *providerClient.OCSProviderClient) (reconcile.Result, error) {

	var blockPoolNames []string
	for _, cephBlockPool := range r.cephBlockPoolList {
		blockPoolNames = append(blockPoolNames, cephBlockPool.Name)
	}

	config, err := ocsClient.GetMirroringInfo(r.ctx, r.storageClusterPeer.Status.PeerID, blockPoolNames)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			r.logGrpcError(GetMirroringInfo, err, st.Code())
		}
		r.Log.Error(err, "Failed to get mirroring info.")
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
			secret.Name = generateSecretName(eResource.Name, r.storageClusterPeer.Spec.APIServerEndpoint)
			secret.Namespace = r.storageClusterPeer.Namespace

			_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, secret, func() error {
				secret.Data = data
				return r.own(secret)
			})
			if err != nil {
				r.Log.Error(err, "Failed to create/update the Bootstrap Secret for CephBlockPool", "CephBlockPool", eResource.Name, "Secret", secret.Name)
				return reconcile.Result{}, err
			}
		}
	}

	return reconcile.Result{}, nil
}

func (r *StorageClusterPeerReconciler) reconcileRBDMirrorDaemon() (reconcile.Result, error) {
	rbdMirror := &rookCephv1.CephRBDMirror{}
	rbdMirror.Name = rBDMirrorName
	rbdMirror.Namespace = r.storageClusterPeer.Namespace

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, rbdMirror, func() error {
		rbdMirror.Spec.Count = 1
		return r.own(rbdMirror)
	})
	if err != nil {
		r.Log.Error(err, "Failed to create/update the CephRBDMirror", "CephRBDMirror", rbdMirror)
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (r *StorageClusterPeerReconciler) reconcileBlockPools() (reconcile.Result, error) {

	for _, cephBlockPool := range r.cephBlockPoolList {

		cephBlockPool.Spec.Mirroring.Enabled = true
		cephBlockPool.Spec.Mirroring.Mode = "image"

		secretName := generateSecretName(cephBlockPool.Name, r.storageClusterPeer.Spec.APIServerEndpoint)

		// set the secret ref
		if cephBlockPool.Spec.Mirroring.Peers == nil {
			cephBlockPool.Spec.Mirroring.Peers = &rookCephv1.MirroringPeerSpec{SecretNames: []string{}}
		}

		if !slices.Contains(cephBlockPool.Spec.Mirroring.Peers.SecretNames, secretName) {
			cephBlockPool.Spec.Mirroring.Peers.SecretNames = append(cephBlockPool.Spec.Mirroring.Peers.SecretNames, secretName)
		}

		err := r.update(cephBlockPool)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to set bootstrap secret ref on CephBlockPool %q: %v", cephBlockPool.Name, err)
		}
	}
	return reconcile.Result{}, nil
}

func (r *StorageClusterPeerReconciler) deleteSecretRefOnBlockPools() (reconcile.Result, error) {

	storageClusterPeerList := &v1.StorageClusterPeerList{}
	if err := r.list(storageClusterPeerList, client.InNamespace(r.storageClusterPeer.Namespace)); err != nil {
		return reconcile.Result{}, err
	}

	for _, cephBlockPool := range r.cephBlockPoolList {

		// If this is the last storageClusterPeer disable mirroring for block Pool
		if len(storageClusterPeerList.Items) <= 1 {
			cephBlockPool.Spec.Mirroring.Enabled = false
			cephBlockPool.Spec.Mirroring.Mode = ""
		}

		secretName := generateSecretName(cephBlockPool.Name, r.storageClusterPeer.Spec.APIServerEndpoint)

		index := slices.IndexFunc(cephBlockPool.Spec.Mirroring.Peers.SecretNames, func(s string) bool {
			return s == secretName
		})
		if index >= 0 {
			cephBlockPool.Spec.Mirroring.Peers.SecretNames = append(
				cephBlockPool.Spec.Mirroring.Peers.SecretNames[:index],
				cephBlockPool.Spec.Mirroring.Peers.SecretNames[index+1:]...)
		}
		err := r.update(cephBlockPool)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to un-set bootstrap secret ref on CephBlockPool %q: %v", cephBlockPool.Name, err)
		}
	}
	return reconcile.Result{}, nil
}

func (r *StorageClusterPeerReconciler) logGrpcError(grpcCallName string, err error, errCode codes.Code) {

	var msg string

	if grpcCallName == OnboardMirrorPeer {
		if errCode == codes.InvalidArgument {
			msg = "Token is invalid. Verify the token again or contact the provider admin"
		} else if errCode == codes.AlreadyExists {
			msg = "Token is already used. Contact provider admin for a new token"
		}
	} else if grpcCallName == AcknowledgeMirrorPeerOnboarding {
		if errCode == codes.NotFound {
			msg = "StorageConsumer not found. Contact the provider admin"
		}
	} else if grpcCallName == OffboardMirrorPeer {
		if errCode == codes.InvalidArgument {
			msg = "StorageConsumer UID is not valid. Contact the provider admin"
		}
	} else if grpcCallName == GetMirroringInfo {
		if errCode == codes.InvalidArgument {
			msg = "StorageConsumer UID is not valid. Contact the provider admin"
		} else if errCode == codes.NotFound {
			msg = "StorageConsumer UID not found. Contact the provider admin"
		} else if errCode == codes.Unavailable {
			msg = "StorageConsumer is not ready yet. Will requeue after 5 second"
		}
	}

	if msg != "" {
		r.Log.Error(err, "StorageProvider:"+grpcCallName+":"+msg)
	}
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

func (r *StorageClusterPeerReconciler) own(resource metav1.Object) error {
	// Ensure StorageClusterPeer ownership on a resource
	return controllerutil.SetControllerReference(r.storageClusterPeer, resource, r.Scheme)
}

func generateSecretName(poolName, apiServerEndpoint string) string {
	md5Sum := md5.Sum([]byte(fmt.Sprintf("%s-%s", poolName, apiServerEndpoint)))
	return fmt.Sprintf("%s-%s", bootstrapSecretPrefix, hex.EncodeToString(md5Sum[:16]))
}
