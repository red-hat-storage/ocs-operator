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

package mirroring

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	providerClient "github.com/red-hat-storage/ocs-operator/services/provider/api/v4/client"
	controllers "github.com/red-hat-storage/ocs-operator/v4/controllers/storageconsumer"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	"github.com/go-logr/logr"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"golang.org/x/exp/maps"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// internalKey is a special key for storage-client-mapping to establish mirroring between blockPools for internal mode
	internalKey                     = "internal"
	mirroringFinalizer              = "ocs.openshift.io/mirroring"
	clientIDIndexName               = "clientID"
	storageClusterPeerAnnotationKey = "ocs.openshift.io/storage-cluster-peer"
)

// MirroringReconciler reconciles a Mirroring fields for Ceph Object(s)
// nolint:revive
type MirroringReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	log logr.Logger
	ctx context.Context
}

// SetupWithManager sets up the controller with the Manager.
func (r *MirroringReconciler) SetupWithManager(mgr ctrl.Manager) error {

	ctx := context.Background()

	if err := mgr.GetCache().IndexField(
		ctx,
		&ocsv1alpha1.StorageConsumer{},
		util.AnnotationIndexName,
		util.AnnotationIndexFieldFunc,
	); err != nil {
		return fmt.Errorf("failed to set up FieldIndexer on StorageConsumer for annotations: %v", err)
	}

	if err := mgr.GetCache().IndexField(
		ctx,
		&ocsv1alpha1.StorageConsumer{},
		clientIDIndexName,
		func(obj client.Object) []string {
			if storageConsumer, ok := obj.(*ocsv1alpha1.StorageConsumer); ok {
				return []string{storageConsumer.Status.Client.ID}
			}
			return nil
		},
	); err != nil {
		return fmt.Errorf("failed to set up FieldIndexer on StorageConsumer for clientID: %v", err)
	}

	// Reconcile the OperatorConfigMap object when the cluster's version object is updated
	enqueueConfigMapRequest := handler.EnqueueRequestsFromMapFunc(
		func(_ context.Context, obj client.Object) []reconcile.Request {
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{
					Name:      util.StorageClientMappingConfigName,
					Namespace: obj.GetNamespace(),
				},
			}}
		},
	)

	generationChangePredicate := builder.WithPredicates(predicate.GenerationChangedPredicate{})

	return ctrl.NewControllerManagedBy(mgr).
		For(
			&corev1.ConfigMap{},
			builder.WithPredicates(util.NamePredicate(util.StorageClientMappingConfigName)),
		).
		Owns(
			&rookCephv1.CephRBDMirror{},
			generationChangePredicate,
		).
		Watches(
			&ocsv1.StorageClusterPeer{},
			enqueueConfigMapRequest,
			generationChangePredicate,
		).
		Watches(
			&ocsv1alpha1.StorageConsumer{},
			enqueueConfigMapRequest,
			builder.WithPredicates(
				predicate.Or(
					predicate.AnnotationChangedPredicate{},
					predicate.GenerationChangedPredicate{},
				),
			),
		).
		Watches(
			&rookCephv1.CephBlockPool{},
			enqueueConfigMapRequest,
			generationChangePredicate,
		).
		Watches(
			&rookCephv1.CephBlockPoolRadosNamespace{},
			enqueueConfigMapRequest,
			generationChangePredicate,
		).
		Complete(r)
}

//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusterpeers;storageconsumers,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=configmaps/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=ceph.rook.io,resources=cephrbdmirrors,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=ceph.rook.io,resources=cephblockpools;cephblockpoolradosnamespaces,verbs=get;list;watch;create;update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *MirroringReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	r.ctx = ctx
	r.log = log.FromContext(ctx, "StorageClientMapping", request.NamespacedName)
	r.log.Info("Starting reconcile")

	clientMappingConfig := &corev1.ConfigMap{}
	clientMappingConfig.Name = request.Name
	clientMappingConfig.Namespace = request.Namespace

	if err := r.get(clientMappingConfig); err != nil {
		if k8serrors.IsNotFound(err) {
			r.log.Info("ConfigMap %s not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		r.log.Error(err, "Failed to get ConfigMap.")
		return ctrl.Result{}, err
	}
	return r.reconcilePhases(clientMappingConfig)
}

func (r *MirroringReconciler) reconcilePhases(clientMappingConfig *corev1.ConfigMap) (ctrl.Result, error) {

	shouldMirror := clientMappingConfig.DeletionTimestamp.IsZero() &&
		clientMappingConfig.Data != nil &&
		len(clientMappingConfig.Data) > 0

	if shouldMirror {
		if controllerutil.AddFinalizer(clientMappingConfig, mirroringFinalizer) {
			r.log.Info("Finalizer not found for ConfigMap. Adding finalizer.")
			if err := r.update(clientMappingConfig); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update ConfigMap: %v", err)
			}
		}
	}

	// Fetch the StorageClusterPeer instance
	if clientMappingConfig.GetAnnotations()[storageClusterPeerAnnotationKey] == "" {
		return ctrl.Result{}, fmt.Errorf("storageClusterPeer reference not found")
	}

	storageClusterPeer := &ocsv1.StorageClusterPeer{}
	storageClusterPeer.Name = clientMappingConfig.GetAnnotations()[storageClusterPeerAnnotationKey]
	storageClusterPeer.Namespace = clientMappingConfig.Namespace

	if err := r.get(storageClusterPeer); err != nil {
		r.log.Error(err, "Failed to get StorageClusterPeer.")
		return ctrl.Result{}, err
	}

	if storageClusterPeer.Status.State != ocsv1.StorageClusterPeerStatePeered ||
		storageClusterPeer.Status.PeerInfo == nil ||
		storageClusterPeer.Status.PeerInfo.StorageClusterUid == "" {
		r.log.Info(
			"waiting for StorageClusterPeer to be in Peered state",
			"StorageClusterPeer",
			storageClusterPeer.Name,
		)
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	errorOccurred := false

	ocsClient, err := providerClient.NewProviderClient(r.ctx, storageClusterPeer.Spec.ApiEndpoint, util.OcsClientTimeout)
	if err != nil {
		r.log.Error(err, "failed to create a new provider client")
		errorOccurred = true
	} else if ocsClient != nil {
		defer ocsClient.Close()
	}

	if errored := r.reconcileRbdMirror(clientMappingConfig, shouldMirror); errored {
		errorOccurred = true
	}

	if errored := r.reconcileBlockPoolMirroring(
		ocsClient,
		clientMappingConfig,
		storageClusterPeer,
		shouldMirror,
	); errored {
		errorOccurred = true
	}

	if errored := r.reconcileRadosNamespaceMirroring(
		ocsClient,
		clientMappingConfig,
		storageClusterPeer,
		shouldMirror,
	); errored {
		errorOccurred = true
	}

	if !shouldMirror {
		if controllerutil.RemoveFinalizer(clientMappingConfig, mirroringFinalizer) {
			r.log.Info("removing finalizer from ConfigMap.")
			if err := r.update(clientMappingConfig); err != nil {
				r.log.Info("Failed to remove finalizer from ConfigMap")
				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer from ConfigMap: %v", err)
			}
		}
	}

	if errorOccurred {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile StorageClientMapping")
	}

	return ctrl.Result{}, nil
}

func (r *MirroringReconciler) reconcileRbdMirror(clientMappingConfig *corev1.ConfigMap, shouldMirror bool) bool {

	rbdMirror := &rookCephv1.CephRBDMirror{}
	rbdMirror.Name = util.CephRBDMirrorName
	rbdMirror.Namespace = clientMappingConfig.Namespace

	storageConsumers := &ocsv1alpha1.StorageConsumerList{}
	if err := r.list(
		storageConsumers,
		client.MatchingFields{util.AnnotationIndexName: util.RequestMaintenanceModeAnnotation},
	); err != nil {
		r.log.Error(err, "failed to list StorageConsumer(s)")
		return true
	}

	maintenanceModeRequested := len(storageConsumers.Items) >= 1

	if shouldMirror && !maintenanceModeRequested {
		_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, rbdMirror, func() error {
			if err := r.own(clientMappingConfig, rbdMirror); err != nil {
				return err
			}
			rbdMirror.Spec.Count = 1
			return nil
		})
		if err != nil {
			r.log.Error(err, "Failed to create/update the CephRBDMirror", "CephRBDMirror", rbdMirror.Name)
			return true
		}
	} else {
		if err := r.delete(rbdMirror); err != nil {
			r.log.Error(err, "failed to delete CephRBDMirror", "CephRBDMirror", rbdMirror.Name)
			return true
		}
	}

	return false
}

func (r *MirroringReconciler) reconcileBlockPoolMirroring(
	ocsClient *providerClient.OCSProviderClient,
	clientMappingConfig *corev1.ConfigMap,
	storageClusterPeer *ocsv1.StorageClusterPeer,
	shouldMirror bool,
) bool {
	errorOccurred := false

	cephBlockPoolsList := &rookCephv1.CephBlockPoolList{}
	if err := r.list(
		cephBlockPoolsList,
		client.InNamespace(clientMappingConfig.Namespace),
	); err != nil {
		r.log.Error(err, "failed to list cephBlockPools")
		return true
	}

	blockPoolByName := map[string]*rookCephv1.CephBlockPool{}
	for i := range cephBlockPoolsList.Items {
		cephBlockPool := &cephBlockPoolsList.Items[i]
		if _, ok := cephBlockPool.GetLabels()[util.ForbidMirroringLabel]; ok {
			continue
		}
		blockPoolByName[cephBlockPool.Name] = cephBlockPool
	}

	if len(blockPoolByName) > 0 && ocsClient != nil {
		// fetch BlockPoolsInfo
		response, err := ocsClient.GetBlockPoolsInfo(
			r.ctx,
			storageClusterPeer.Status.PeerInfo.StorageClusterUid,
			maps.Keys(blockPoolByName),
		)
		if err != nil {
			r.log.Error(err, "failed to get CephBlockPool(s) info from Peer")
			return true
		}

		if response.Errors != nil {
			for i := range response.Errors {
				resp := response.Errors[i]
				r.log.Error(
					errors.New(resp.Message),
					"failed to get BlockPoolsInfo",
					"CephBlockPool",
					resp.BlockPoolName,
				)
			}
			errorOccurred = true
		}

		if shouldMirror {
			for i := range response.BlockPoolsInfo {
				blockPoolName := response.BlockPoolsInfo[i].BlockPoolName
				cephBlockPool := blockPoolByName[blockPoolName]

				mirroringToken := response.BlockPoolsInfo[i].MirroringToken
				secretName := GetMirroringSecretName(blockPoolName)
				if mirroringToken != "" {
					mirroringSecret := &corev1.Secret{}
					mirroringSecret.Name = secretName
					mirroringSecret.Namespace = clientMappingConfig.Namespace
					_, err = ctrl.CreateOrUpdate(r.ctx, r.Client, mirroringSecret, func() error {
						if err = r.own(clientMappingConfig, mirroringSecret); err != nil {
							return err
						}
						mirroringSecret.Data = map[string][]byte{
							"pool":  []byte(blockPoolName),
							"token": []byte(mirroringToken),
						}
						return nil
					})
					if err != nil {
						r.log.Error(err, "failed to create/update mirroring secret", "Secret", secretName)
						errorOccurred = true
						continue
					}
				} else {
					// Mirroring Token is generated only after mirroring is enabled. If both sides reconcile at the
					// same time the mirroring token won't be fetched. Hence, re-queuing to avoid this
					r.log.Error(
						fmt.Errorf("peer's CephBlockPool mirroring token is not generated"),
						"Re-queuing as peer's CephBlockPool mirroring token is not generated",
					)
					errorOccurred = true
				}

				// We need to enable mirroring for the blockPool, else the mirroring secret will not be generated
				_, err = controllerutil.CreateOrUpdate(r.ctx, r.Client, cephBlockPool, func() error {
					util.AddAnnotation(
						cephBlockPool,
						util.BlockPoolMirroringTargetIDAnnotation,
						response.BlockPoolsInfo[i].BlockPoolID,
					)

					cephBlockPool.Spec.Mirroring.Enabled = true
					cephBlockPool.Spec.Mirroring.Mode = "image"
					if mirroringToken != "" {
						if cephBlockPool.Spec.Mirroring.Peers == nil {
							cephBlockPool.Spec.Mirroring.Peers = &rookCephv1.MirroringPeerSpec{SecretNames: []string{}}
						}
						if !slices.Contains(cephBlockPool.Spec.Mirroring.Peers.SecretNames, secretName) {
							cephBlockPool.Spec.Mirroring.Peers.SecretNames = append(
								cephBlockPool.Spec.Mirroring.Peers.SecretNames, secretName)
						}
					}
					return nil
				})
				if err != nil {
					r.log.Error(
						err,
						"failed to update CephBlockPool for mirroring",
						"CephBlockPool",
						cephBlockPool.Name,
					)
					errorOccurred = true
				}
			}
		} else {
			for i := range response.BlockPoolsInfo {
				blockPoolName := response.BlockPoolsInfo[i].BlockPoolName
				cephBlockPool := blockPoolByName[blockPoolName]
				_, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, cephBlockPool, func() error {
					cephBlockPool.Spec.Mirroring = rookCephv1.MirroringSpec{}
					return nil
				})
				if err != nil {
					r.log.Error(
						err,
						"failed to disable mirroring for CephBlockPool",
						"CephBlockPool",
						cephBlockPool.Name,
					)
					errorOccurred = true
				}
			}
		}
	}

	return errorOccurred
}

func (r *MirroringReconciler) reconcileRadosNamespaceMirroring(
	ocsClient *providerClient.OCSProviderClient,
	clientMappingConfig *corev1.ConfigMap,
	storageClusterPeer *ocsv1.StorageClusterPeer,
	shouldMirror bool,
) bool {
	/*
		Algorithm:
		make a list of peerClientIDs
		send GetStorageClientsInfo with this Info
		make a map of remoteNamespaceByClientID from response
		list all radosNamespace and for each,
			find out which consumer does it belong to
			find the localClientID and use map to get radosnamespce
			if radosnamespace is empty, disable mirroring
			else enable mirroring
	*/
	errorOccurred := false

	peerClientIDs := []string{}
	storageConsumerByName := map[string]*ocsv1alpha1.StorageConsumer{}
	for localClientID, peerClientID := range clientMappingConfig.Data {
		// for internal mode, we need only blockPool mirroring, hence skipping this for the special key "internal"
		if localClientID == internalKey {
			continue
		}
		// Check if the storageConsumer with the ClientID exists, this is a fancy get operation as
		// there will be only one consumer with the clientID
		storageConsumers := &ocsv1alpha1.StorageConsumerList{}
		if err := r.list(
			storageConsumers,
			client.MatchingFields{clientIDIndexName: localClientID},
			client.Limit(2),
		); err != nil {
			r.log.Error(err, "failed to list the StorageConsumer")
			errorOccurred = true
			continue
		}
		if len(storageConsumers.Items) != 1 {
			r.log.Error(
				fmt.Errorf("invalid number of StorageConumers found"),
				fmt.Sprintf("expected 1 StorageConsumer but got %v", len(storageConsumers.Items)),
			)
			errorOccurred = true
			continue
		}
		storageConsumerByName[storageConsumers.Items[0].Name] = &storageConsumers.Items[0]
		peerClientIDs = append(peerClientIDs, peerClientID)
	}

	if len(peerClientIDs) > 0 && ocsClient != nil {
		response, err := ocsClient.GetStorageClientsInfo(
			r.ctx,
			storageClusterPeer.Status.PeerInfo.StorageClusterUid,
			peerClientIDs,
		)
		if err != nil {
			r.log.Error(err, "failed to get StorageClient(s) info from Peer")
			return true
		}

		if response.Errors != nil {
			for i := range response.Errors {
				resp := response.Errors[i]
				r.log.Error(
					errors.New(resp.Message),
					"failed to get StorageClientsInfo",
					"CephBlockPool",
					resp.ClientID,
				)
			}
			errorOccurred = true
		}

		remoteNamespaceByClientID := map[string]string{}
		for i := range response.ClientsInfo {
			clientInfo := response.ClientsInfo[i]
			remoteNamespaceByClientID[clientInfo.ClientID] = clientInfo.RadosNamespace
		}

		radosNamespaceList := &rookCephv1.CephBlockPoolRadosNamespaceList{}
		if err = r.list(radosNamespaceList, client.InNamespace(storageClusterPeer.Namespace)); err != nil {
			r.log.Error(err, "failed to list CephBlockPoolRadosNamespace(s)")
			return true
		}

		for i := range radosNamespaceList.Items {
			rns := &radosNamespaceList.Items[i]
			consumer := storageConsumerByName[rns.GetLabels()[controllers.StorageConsumerNameLabel]]
			if consumer == nil || consumer.Status.Client.ID == "" {
				continue
			}
			remoteClientID := clientMappingConfig.Data[consumer.Status.Client.ID]
			remoteNamespace := remoteNamespaceByClientID[remoteClientID]
			_, err = controllerutil.CreateOrUpdate(r.ctx, r.Client, rns, func() error {
				if remoteNamespace != "" && shouldMirror {
					rns.Spec.Mirroring = &rookCephv1.RadosNamespaceMirroring{
						RemoteNamespace: ptr.To(remoteNamespace),
						Mode:            "image",
					}
				} else {
					rns.Spec.Mirroring = nil
				}
				return nil
			})
			if err != nil {
				r.log.Error(
					err,
					"failed to update radosnamespace",
					"CephBlockPoolRadosNamespace",
					rns.Name,
				)
				errorOccurred = true
			}
		}
	}
	return errorOccurred
}

func (r *MirroringReconciler) get(obj client.Object) error {
	return r.Client.Get(r.ctx, client.ObjectKeyFromObject(obj), obj)
}

func (r *MirroringReconciler) list(obj client.ObjectList, listOptions ...client.ListOption) error {
	return r.Client.List(r.ctx, obj, listOptions...)
}

func (r *MirroringReconciler) update(obj client.Object, opts ...client.UpdateOption) error {
	return r.Client.Update(r.ctx, obj, opts...)
}

func (r *MirroringReconciler) delete(obj client.Object, opts ...client.DeleteOption) error {
	return r.Client.Delete(r.ctx, obj, opts...)
}

func (r *MirroringReconciler) own(owner *corev1.ConfigMap, obj client.Object) error {
	return controllerutil.SetControllerReference(owner, obj, r.Scheme)
}

func GetMirroringSecretName(blockPoolName string) string {
	return fmt.Sprintf("rbd-mirroring-token-%s", util.CalculateMD5Hash(blockPoolName))
}
