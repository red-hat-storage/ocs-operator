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
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	pb "github.com/red-hat-storage/ocs-operator/services/provider/api/v4"
	providerClient "github.com/red-hat-storage/ocs-operator/services/provider/api/v4/client"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	"github.com/go-logr/logr"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
			if storageConsumer, ok := obj.(*ocsv1alpha1.StorageConsumer); ok && storageConsumer.Status.Client != nil {
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
		Named("MirroringController").
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

	// Fetch the StorageClusterPeer instance
	if clientMappingConfig.GetAnnotations()[storageClusterPeerAnnotationKey] == "" {
		return ctrl.Result{}, fmt.Errorf("storageClusterPeer reference not found")
	}

	storageClusterPeer := &ocsv1.StorageClusterPeer{}
	storageClusterPeer.Name = clientMappingConfig.GetAnnotations()[storageClusterPeerAnnotationKey]
	storageClusterPeer.Namespace = clientMappingConfig.Namespace

	if err := r.get(storageClusterPeer); client.IgnoreNotFound(err) != nil {
		r.log.Error(err, "Failed to get StorageClusterPeer.")
		return ctrl.Result{}, err
	}

	storageConsumerList := &ocsv1alpha1.StorageConsumerList{}
	if err := r.list(storageConsumerList, client.InNamespace(clientMappingConfig.Namespace)); err != nil {
		r.log.Error(err, "Failed to list StorageConsumers")
		return ctrl.Result{}, err
	}

	remoteClientIds := []string{}
	storageConsumerByName := map[string]*ocsv1alpha1.StorageConsumer{}
	for i := range storageConsumerList.Items {
		consumer := &storageConsumerList.Items[i]
		storageConsumerByName[consumer.Name] = consumer
		if cl := consumer.Status.Client; cl != nil && clientMappingConfig.Data[cl.ID] != "" {
			remoteClientIds = append(remoteClientIds, clientMappingConfig.Data[cl.ID])
		}
	}

	cephBlockPoolsList := &rookCephv1.CephBlockPoolList{}
	if err := r.list(cephBlockPoolsList, client.InNamespace(clientMappingConfig.Namespace)); err != nil {
		r.log.Error(err, "Failed to list CephBlockPools")
		return ctrl.Result{}, err
	}

	remoteClientInfoById := map[string]*pb.ClientInfo{}
	remoteBlockPoolInfoByName := map[string]*pb.BlockPoolInfo{}
	errorOccurred := false

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
		if controllerutil.AddFinalizer(storageClusterPeer, mirroringFinalizer) {
			r.log.Info("Finalizer not found for StorageClusterPeer. Adding finalizer.")
			if err := r.update(storageClusterPeer); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update StorageClusterPeer: %v", err)
			}
		}

		if storageClusterPeer.Status.State != ocsv1.StorageClusterPeerStatePeered ||
			storageClusterPeer.Status.PeerInfo == nil ||
			storageClusterPeer.Status.PeerInfo.StorageClusterUid == "" {
			r.log.Info("Waiting for StorageClusterPeer to be in Peered state", "StorageClusterPeer", storageClusterPeer.Name)
			return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
		}

		ocsClient, err := providerClient.NewProviderClient(r.ctx, storageClusterPeer.Spec.ApiEndpoint, util.OcsClientTimeout)
		if err != nil {
			r.log.Error(err, "failed to create a new provider client")
			return ctrl.Result{}, fmt.Errorf("failed to reconcile StorageClientMapping")
		} else if ocsClient != nil {
			defer ocsClient.Close()
		}

		if errored := r.getStorageClientsInfo(ocsClient, storageClusterPeer, remoteClientIds, remoteClientInfoById); errored {
			errorOccurred = true
		}

		if errored := r.getBlockPoolsInfo(ocsClient, storageClusterPeer, cephBlockPoolsList, remoteBlockPoolInfoByName); errored {
			errorOccurred = true
		}
	}

	if errored := r.reconcileStorageConsumer(storageConsumerList, clientMappingConfig, remoteClientInfoById); errored {
		errorOccurred = true
	}
	if errored := r.reconcileRadosNamespaceMirroring(clientMappingConfig, storageConsumerByName, remoteClientInfoById); errored {
		errorOccurred = true
	}

	if errored := r.reconcileBlockPoolMirroring(clientMappingConfig, cephBlockPoolsList, remoteBlockPoolInfoByName); errored {
		errorOccurred = true
	}

	if errored := r.reconcileRbdMirror(clientMappingConfig, shouldMirror); errored {
		errorOccurred = true
	}

	if !shouldMirror {
		if controllerutil.RemoveFinalizer(storageClusterPeer, mirroringFinalizer) {
			r.log.Info("removing finalizer from StorageClusterPeer.")
			if err := r.update(storageClusterPeer); err != nil {
				r.log.Error(err, "Failed to remove finalizer from StorageClusterPeer")
				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer from StorageClusterPeer: %v", err)
			}
		}
		if controllerutil.RemoveFinalizer(clientMappingConfig, mirroringFinalizer) {
			r.log.Info("removing finalizer from ConfigMap.")
			if err := r.update(clientMappingConfig); err != nil {
				r.log.Error(err, "Failed to remove finalizer from ConfigMap")
				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer from ConfigMap: %v", err)
			}
		}
	}

	if errorOccurred {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile StorageClientMapping")
	}

	return ctrl.Result{}, nil
}

func (r *MirroringReconciler) getBlockPoolsInfo(
	ocsClient *providerClient.OCSProviderClient,
	storageClusterPeer *ocsv1.StorageClusterPeer,
	cephBlockPoolsList *rookCephv1.CephBlockPoolList,
	remoteBlockPoolInfoByName map[string]*pb.BlockPoolInfo,
) bool {
	blockPoolNames := []string{}
	errorOccurred := false
	for i := range cephBlockPoolsList.Items {
		cephBlockPool := &cephBlockPoolsList.Items[i]
		labels := cephBlockPool.GetLabels()
		forInternalUseOnly, _ := strconv.ParseBool(labels[util.ForInternalUseOnlyLabelKey])
		forbidMirroring, _ := strconv.ParseBool(labels[util.ForbidMirroringLabel])
		if forInternalUseOnly || forbidMirroring {
			continue
		}
		blockPoolNames = append(blockPoolNames, cephBlockPool.Name)
	}
	// fetch BlockPoolsInfo
	response, err := ocsClient.GetBlockPoolsInfo(
		r.ctx,
		storageClusterPeer.Status.PeerInfo.StorageClusterUid,
		blockPoolNames,
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

	for i := range response.BlockPoolsInfo {
		remoteBlockPoolInfo := response.BlockPoolsInfo[i]
		remoteBlockPoolInfoByName[remoteBlockPoolInfo.BlockPoolName] = response.BlockPoolsInfo[i]
	}

	return errorOccurred
}

func (r *MirroringReconciler) getStorageClientsInfo(
	ocsClient *providerClient.OCSProviderClient,
	storageClusterPeer *ocsv1.StorageClusterPeer,
	remoteClientIds []string,
	remoteClientIdByClientInfo map[string]*pb.ClientInfo,
) bool {
	errorOccurred := false
	response, err := ocsClient.GetStorageClientsInfo(
		r.ctx,
		storageClusterPeer.Status.PeerInfo.StorageClusterUid,
		remoteClientIds,
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
				"StorageClient",
				resp.ClientID,
			)
		}
		errorOccurred = true
	}

	for i := range response.ClientsInfo {
		remoteClientIdByClientInfo[response.ClientsInfo[i].ClientID] = response.ClientsInfo[i]
	}

	return errorOccurred
}

func (r *MirroringReconciler) reconcileRbdMirror(clientMappingConfig *corev1.ConfigMap, shouldMirror bool) bool {
	rbdMirrorList := &rookCephv1.CephRBDMirrorList{}

	if err := r.list(
		rbdMirrorList,
		client.InNamespace(clientMappingConfig.Namespace),
		client.Limit(2),
	); err != nil {
		r.log.Error(err, "Failed to list RBDMirror.")
		return true
	} else if len(rbdMirrorList.Items) == 2 {
		r.log.Error(fmt.Errorf("multiple RBDMirror present in the cluster"), "more than 1 CephRBDMirror present in the cluster")
		return true
	} else if len(rbdMirrorList.Items) == 1 {
		if rbdMirrorList.Items[0].Name != util.CephRBDMirrorName {
			r.log.Error(fmt.Errorf("RBDMirror name mismatch"), "RBDMirror with a different name is present in the cluster")
			return true
		}
	}

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
	clientMappingConfig *corev1.ConfigMap,
	cephBlockPoolsList *rookCephv1.CephBlockPoolList,
	remoteBlockPoolInfoByName map[string]*pb.BlockPoolInfo,
) bool {
	errorOccurred := false

	for i := range cephBlockPoolsList.Items {
		cephBlockPool := &cephBlockPoolsList.Items[i]

		labels := cephBlockPool.GetLabels()
		forInternalUseOnly, _ := strconv.ParseBool(labels[util.ForInternalUseOnlyLabelKey])
		forbidMirroring, _ := strconv.ParseBool(labels[util.ForbidMirroringLabel])
		blockPoolInfo := remoteBlockPoolInfoByName[cephBlockPool.Name]

		if blockPoolInfo == nil || forbidMirroring || forInternalUseOnly {
			_, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, cephBlockPool, func() error {
				delete(cephBlockPool.GetLabels(), util.BlockPoolMirroringTargetIDAnnotation)
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
		} else {
			mirroringToken := blockPoolInfo.MirroringToken
			secretName := GetMirroringSecretName(cephBlockPool.Name)
			if mirroringToken != "" {
				mirroringSecret := &corev1.Secret{}
				mirroringSecret.Name = secretName
				mirroringSecret.Namespace = clientMappingConfig.Namespace
				_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, mirroringSecret, func() error {
					if err := r.own(clientMappingConfig, mirroringSecret); err != nil {
						return err
					}
					mirroringSecret.Data = map[string][]byte{
						"pool":  []byte(cephBlockPool.Name),
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
			_, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, cephBlockPool, func() error {
				util.AddAnnotation(
					cephBlockPool,
					util.BlockPoolMirroringTargetIDAnnotation,
					blockPoolInfo.BlockPoolID,
				)

				cephBlockPool.Spec.Mirroring.Enabled = true
				cephBlockPool.Spec.Mirroring.Mode = "init-only"
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

	}

	return errorOccurred
}

func (r *MirroringReconciler) reconcileRadosNamespaceMirroring(
	clientMappingConfig *corev1.ConfigMap,
	storageConsumerByName map[string]*ocsv1alpha1.StorageConsumer,
	remoteClientInfoById map[string]*pb.ClientInfo,
) bool {
	errorOccurred := false

	radosNamespaceList := &rookCephv1.CephBlockPoolRadosNamespaceList{}
	if err := r.list(radosNamespaceList, client.InNamespace(clientMappingConfig.Namespace)); err != nil {
		r.log.Error(err, "Failed to list CephBlockPools")
		return true
	}

	for i := range radosNamespaceList.Items {
		rns := &radosNamespaceList.Items[i]
		consumerIndex := slices.IndexFunc(
			rns.OwnerReferences,
			func(ref metav1.OwnerReference) bool { return ref.Kind == "StorageConsumer" },
		)
		if consumerIndex == -1 {
			continue
		}
		consumerOwner := &rns.OwnerReferences[consumerIndex]
		consumer := storageConsumerByName[consumerOwner.Name]
		remoteNamespace := ""
		if consumer != nil && consumer.Status.Client != nil {
			remoteClientId := clientMappingConfig.Data[consumer.Status.Client.ID]
			if remoteClientInfo := remoteClientInfoById[remoteClientId]; remoteClientInfo != nil {
				remoteNamespace = remoteClientInfo.RadosNamespace
			}
		}
		_, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, rns, func() error {
			if remoteNamespace != "" {
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

	return errorOccurred
}

func (r *MirroringReconciler) reconcileStorageConsumer(
	storageConsumerList *ocsv1alpha1.StorageConsumerList,
	clientMappingConfig *corev1.ConfigMap,
	remoteClientInfoById map[string]*pb.ClientInfo,
) bool {
	errorOccurred := false

	for i := range storageConsumerList.Items {
		consumer := &storageConsumerList.Items[i]
		var clientInfo *pb.ClientInfo
		if cl := consumer.Status.Client; cl != nil {
			if remoteClientId := clientMappingConfig.Data[cl.ID]; remoteClientId != "" {
				clientInfo = remoteClientInfoById[remoteClientId]
			}
		}
		updateRequired := false
		if clientInfo == nil {
			if _, hasAnnotation := consumer.GetAnnotations()[util.StorageConsumerMirroringInfoAnnotation]; hasAnnotation {
				updateRequired = true
			}
		} else {
			marshaledClientInfo, err := json.Marshal(clientInfo)
			if err != nil {
				panic("failed to marshal")
			}
			if util.AddAnnotation(consumer, util.StorageConsumerMirroringInfoAnnotation, string(marshaledClientInfo)) {
				updateRequired = true
			}
		}
		if updateRequired {
			if err := r.update(consumer); err != nil {
				r.log.Error(
					err,
					"failed to update StorageConsumer with mirroring info annotation",
					"StorageConsumer",
					client.ObjectKeyFromObject(consumer),
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
