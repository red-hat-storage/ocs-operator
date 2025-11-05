/*
Copyright 2021 Red Hat OpenShift Container Storage.

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

package controllers

import (
	"cmp"
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/version"

	"github.com/go-logr/logr"
	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"go.uber.org/multierr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	onboardingPrivateKeyFilePath  = "/etc/private-key/key"
	StorageConsumerAnnotation     = "ocs.openshift.io.storageconsumer"
	StorageCephUserTypeAnnotation = "ocs.openshift.io.cephusertype"
	StorageProfileLabel           = "ocs.openshift.io/storageprofile"
	ConsumerUUIDLabel             = "ocs.openshift.io/storageconsumer-uuid"
	StorageConsumerNameLabel      = "ocs.openshift.io/storageconsumer-name"
	storageConsumerFinalizer      = "ocs.openshift.io/storageconsumer-protection"
	primaryConsumerUIDAnnotation  = "ocs.openshift.io/primary-consumer-uid"
	blockPoolNameLabel            = "ocs.openshift.io/cephblockpool-name"
	csiCephUserCurrGen            = 1
)

// StorageConsumerReconciler reconciles a StorageConsumer object
type StorageConsumerReconciler struct {
	client.Client
	Log                  logr.Logger
	Scheme               *runtime.Scheme
	TokenLifetimeInHours int

	ctx             context.Context
	storageConsumer *ocsv1alpha1.StorageConsumer
	namespace       string
}

// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageconsumers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageconsumers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=noobaa.io,resources=noobaaaccounts,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephclients,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephfilesystemsubvolumegroups;cephblockpoolradosnamespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephblockpools;cephfilesystems;cephnfses,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;delete

// Reconcile reads that state of the cluster for a StorageConsumer object and makes changes based on the state read
// and what is in the StorageConsumer.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *StorageConsumerReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {

	prevLogger := r.Log
	defer func() { r.Log = prevLogger }()
	r.Log = r.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	r.ctx = ctx
	r.namespace = request.Namespace

	r.storageConsumer = &ocsv1alpha1.StorageConsumer{}
	r.storageConsumer.Name = request.Name
	r.storageConsumer.Namespace = r.namespace

	if err := r.get(r.storageConsumer); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("No StorageConsumer resource.")
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		r.Log.Error(err, "Failed to retrieve StorageConsumer.")
		return reconcile.Result{}, err
	}

	// Reconcile changes to the cluster
	result, reconcileError := r.reconcilePhases()

	// Apply status changes to the StorageConsumer
	statusError := r.Client.Status().Update(r.ctx, r.storageConsumer)
	if statusError != nil {
		r.Log.Info("Could not update StorageConsumer status.")
	}

	// Reconcile errors have higher priority than status update errors
	if reconcileError != nil {
		return reconcile.Result{}, reconcileError
	} else if statusError != nil {
		return reconcile.Result{}, statusError
	}

	return result, nil

}

func (r *StorageConsumerReconciler) reconcilePhases() (reconcile.Result, error) {

	if _, exist := r.storageConsumer.GetLabels()[util.CreatedAtDfVersionLabelKey]; !exist {
		majorAndMinorVersion, err := version.GetMajorAndMinorVersion()
		if err != nil {
			return reconcile.Result{}, err
		}
		if util.AddLabel(r.storageConsumer, util.CreatedAtDfVersionLabelKey, majorAndMinorVersion) {
			if err := r.Update(r.ctx, r.storageConsumer); err != nil {
				return reconcile.Result{}, nil
			}
		}
	}

	if r.storageConsumer.Spec.Enable {
		return r.reconcileEnabledPhases()
	} else {
		return r.reconcileNotEnabledPhases()
	}
}

func (r *StorageConsumerReconciler) reconcileNotEnabledPhases() (reconcile.Result, error) {
	if r.storageConsumer.GetDeletionTimestamp().IsZero() {
		r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateNotEnabled
		if err := r.reconcileOnboardingSecret(); err != nil {
			return reconcile.Result{}, err
		}
	} else {
		r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateDeleting
	}
	return reconcile.Result{}, nil
}

func (r *StorageConsumerReconciler) reconcileEnabledPhases() (reconcile.Result, error) {

	consumerConfigMap := &corev1.ConfigMap{}
	consumerConfigMap.Namespace = r.namespace
	consumerConfigMap.Name = cmp.Or(
		r.storageConsumer.Spec.ResourceNameMappingConfigMap.Name,
		fmt.Sprintf("storageconsumer-%v", util.FnvHash(r.storageConsumer.Name)),
	)
	if err := r.get(consumerConfigMap); client.IgnoreNotFound(err) != nil {
		return reconcile.Result{}, err
	}

	if r.storageConsumer.GetDeletionTimestamp().IsZero() {
		if controllerutil.AddFinalizer(r.storageConsumer, storageConsumerFinalizer) {
			r.Log.Info("Finalizer not found for StorageConsumer. Adding finalizer.")
			if err := r.Client.Update(r.ctx, r.storageConsumer); err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to update StorageConsumer: %v", err)
			}
		}

		r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateConfiguring
		if err := r.deleteOnboardingSecret(); err != nil {
			return reconcile.Result{}, nil
		}

		storageCluster, err := util.GetStorageClusterInNamespace(r.ctx, r.Client, r.namespace)
		if err != nil {
			return reconcile.Result{}, err
		}

		availableServices, err := util.GetAvailableServices(r.ctx, r.Client, storageCluster)
		if err != nil {
			return reconcile.Result{}, err
		}

		if err := r.reconcileConsumerConfigMap(availableServices, consumerConfigMap); err != nil {
			return reconcile.Result{}, err
		}

		primaryConsumerUID := consumerConfigMap.GetAnnotations()[primaryConsumerUIDAnnotation]

		isPrimaryConsumer := primaryConsumerUID == string(r.storageConsumer.UID)

		consumerResources := util.WrapStorageConsumerResourceMap(consumerConfigMap.Data)

		if availableServices.Rbd {
			if err := r.reconcileCephClientRBDProvisioner(
				util.GenerateCsiRbdProvisionerCephClientName(csiCephUserCurrGen, r.storageConsumer.UID),
				consumerResources.GetRbdRadosNamespaceName(),
				consumerConfigMap,
				csiCephUserCurrGen,
			); err != nil {
				return reconcile.Result{}, err
			}
			if err := r.reconcileCephClientRBDNode(
				util.GenerateCsiRbdNodeCephClientName(csiCephUserCurrGen, r.storageConsumer.UID),
				consumerResources.GetRbdRadosNamespaceName(),
				consumerConfigMap,
				csiCephUserCurrGen,
			); err != nil {
				return reconcile.Result{}, err
			}
		}

		if availableServices.CephFs {
			if err := r.reconcileCephClientCephFSProvisioner(
				util.GenerateCsiCephFsProvisionerCephClientName(csiCephUserCurrGen, r.storageConsumer.UID),
				util.GenerateNameForCephFilesystem(storageCluster.Name),
				consumerResources.GetSubVolumeGroupName(),
				consumerConfigMap,
				csiCephUserCurrGen,
			); err != nil {
				return reconcile.Result{}, err
			}
			if err := r.reconcileCephClientCephFSNode(
				util.GenerateCsiCephFsNodeCephClientName(csiCephUserCurrGen, r.storageConsumer.UID),
				util.GenerateNameForCephFilesystem(storageCluster.Name),
				consumerResources.GetSubVolumeGroupName(),
				consumerConfigMap,
				csiCephUserCurrGen,
			); err != nil {
				return reconcile.Result{}, err
			}
		}

		if availableServices.Nfs {
			if err := r.reconcileCephClientNfsProvisioner(
				util.GenerateCsiNfsProvisionerCephClientName(csiCephUserCurrGen, r.storageConsumer.UID),
				util.GenerateNameForCephFilesystem(storageCluster.Name),
				consumerResources.GetSubVolumeGroupName(),
				consumerConfigMap,
				csiCephUserCurrGen,
			); err != nil {
				return reconcile.Result{}, err
			}
			if err := r.reconcileCephClientNfsNode(
				util.GenerateCsiNfsNodeCephClientName(csiCephUserCurrGen, r.storageConsumer.UID),
				util.GenerateNameForCephFilesystem(storageCluster.Name),
				consumerResources.GetSubVolumeGroupName(),
				consumerConfigMap,
				csiCephUserCurrGen,
			); err != nil {
				return reconcile.Result{}, err
			}
		}

		if isPrimaryConsumer {
			if availableServices.Rbd {
				builtinBlockPools := []string{
					"builtin-mgr",
					util.GenerateNameForCephNFSBlockPool(storageCluster),
				}
				if err := r.reconcileCephRadosNamespace(
					consumerResources.GetRbdRadosNamespaceName(),
					consumerConfigMap,
					builtinBlockPools,
				); err != nil {
					return reconcile.Result{}, err
				}
			}
			if availableServices.CephFs {
				if err := r.reconcileCephFilesystemSubVolumeGroup(
					util.GenerateNameForCephFilesystem(storageCluster.Name),
					consumerResources.GetSubVolumeGroupName(),
					consumerConfigMap,
				); err != nil {
					return reconcile.Result{}, err
				}
			}
		}

		clientStatus := r.storageConsumer.Status.Client
		if availableServices.Mcg && clientStatus != nil {
			// A provider cluster already has a NooBaa system and does not require a NooBaa account
			// to connect to a remote cluster, unlike client clusters.
			// A NooBaa account only needs to be created if the storage consumer is for a client cluster.
			if false {
				clusterID := util.GetClusterID(r.ctx, r.Client, &r.Log)
				if clusterID != "" &&
					clientStatus.ClusterID != "" &&
					clientStatus.ClusterID != clusterID {
					if err := r.reconcileNoobaaAccount(); err != nil {
						return reconcile.Result{}, err
					}
				}
			}
		}

		r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateReady

	} else {
		r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateDeleting
		_, hasForceDeleteAnnotation := r.storageConsumer.GetAnnotations()[util.ForceDeletionAnnotationKey]

		consumerOwners := 0
		for i := range consumerConfigMap.OwnerReferences {
			if consumerConfigMap.OwnerReferences[i].Kind == "StorageConsumer" {
				consumerOwners += 1
			}
		}

		if consumerOwners <= 1 && hasForceDeleteAnnotation {
			blockPools := &rookCephv1.CephBlockPoolList{}
			if err := r.List(r.ctx, blockPools, client.InNamespace(r.namespace)); err != nil {
				return reconcile.Result{}, err
			}
			consumerResources := util.WrapStorageConsumerResourceMap(consumerConfigMap.Data)
			radosNamespaceName := consumerResources.GetRbdRadosNamespaceName()

			annotationPatch := client.RawPatch(types.MergePatchType, util.JsonMustMarshal(
				map[string]any{
					"metadata": map[string]any{
						"annotations": map[string]string{
							util.RookForceDeletionAnnotationKey: strconv.FormatBool(true),
						},
					},
				}),
			)

			for idx := range blockPools.Items {
				bp := &blockPools.Items[idx]
				rns := &rookCephv1.CephBlockPoolRadosNamespace{}
				rns.Name = fmt.Sprintf("%s-%s", bp.Name, radosNamespaceName)
				if radosNamespaceName == util.ImplicitRbdRadosNamespaceName {
					rns.Name = fmt.Sprintf(
						"%s-builtin-%s",
						bp.Name,
						util.ImplicitRbdRadosNamespaceName[1:len(util.ImplicitRbdRadosNamespaceName)-1],
					)
				}
				rns.Namespace = r.namespace
				if err := r.Client.Patch(r.ctx, rns, annotationPatch); client.IgnoreNotFound(err) != nil {
					return reconcile.Result{}, fmt.Errorf("failed to annotate CephBlockPoolRadosNamespace: %v", err)
				}
			}

			svg := &rookCephv1.CephFilesystemSubVolumeGroup{}
			svg.Name = consumerResources.GetSubVolumeGroupName()
			svg.Namespace = r.namespace
			if err := r.Client.Patch(r.ctx, svg, annotationPatch); client.IgnoreNotFound(err) != nil {
				return reconcile.Result{}, fmt.Errorf("failed to annotate CephFilesystemSubVolumeGroup: %v", err)
			}
		}

		if r.storageConsumer.Status.Client == nil || hasForceDeleteAnnotation {
			if controllerutil.RemoveFinalizer(r.storageConsumer, storageConsumerFinalizer) {
				r.Log.Info("removing finalizer from StorageConsumer.")
				if err := r.Client.Update(r.ctx, r.storageConsumer); err != nil {
					r.Log.Info("Failed to remove finalizer from StorageConsumer")
					return reconcile.Result{}, fmt.Errorf("failed to remove finalizer from StorageConsumer: %v", err)
				}
			}
		}
	}

	return reconcile.Result{}, nil
}

func (r *StorageConsumerReconciler) reconcileOnboardingSecret() error {
	secret := &corev1.Secret{}
	secret.Name = fmt.Sprintf("onboarding-token-%s", r.storageConsumer.UID)
	secret.Namespace = r.storageConsumer.Namespace
	if _, err := ctrl.CreateOrUpdate(r.ctx, r.Client, secret, func() error {
		if err := r.own(secret); err != nil {
			return err
		}
		// do not overwrite token if already exists
		if len(secret.Data[defaults.OnboardingTokenKey]) > 0 {
			return nil
		}
		token, err := util.GenerateClientOnboardingToken(
			r.TokenLifetimeInHours,
			onboardingPrivateKeyFilePath,
			r.storageConsumer.Name,
		)
		if err != nil {
			return err
		}
		secret.StringData = map[string]string{
			defaults.OnboardingTokenKey: token,
		}
		tokenExpiry := time.Now().Add(time.Duration(r.TokenLifetimeInHours) * time.Hour).UTC()
		util.AddAnnotation(secret, "ocs.openshift.io/token-expiry", tokenExpiry.Format(time.RFC3339))
		return nil
	}); err != nil {
		return fmt.Errorf("failed to create/update secret %s: %v", secret.Name, err)
	}
	r.storageConsumer.Status.OnboardingTicketSecret = corev1.LocalObjectReference{Name: secret.Name}
	return nil
}

func (r *StorageConsumerReconciler) deleteOnboardingSecret() error {
	secret := &corev1.Secret{}
	secret.Name = fmt.Sprintf("onboarding-token-%s", r.storageConsumer.UID)
	secret.Namespace = r.storageConsumer.Namespace
	// once the .spec.enable is true we need to delete secret which is
	// a direct call to k8s api server throughout the storageconsumer(s) lifecycle
	// which adds a log to api server w/ 404 errors as a side effect.
	// get is used to remove the side effect to the maximum possible, get hits
	// cache first and slowly (let's say <5min, much lesser than storageconsumer lifecycle)
	// syncs the delete event as well, from then we'll not make any delete calls.
	if err := r.get(secret); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to get secret %s: %v", secret.Name, err)
	} else if secret.UID != "" {
		if err := r.Delete(r.ctx, secret); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	r.storageConsumer.Status.OnboardingTicketSecret = corev1.LocalObjectReference{}
	return nil
}

func (r *StorageConsumerReconciler) reconcileConsumerConfigMap(
	availableServices *util.AvailableServices,
	consumerConfigMap *corev1.ConfigMap,
) error {

	if _, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, consumerConfigMap, func() error {

		// Get config map's controller reference
		controllerIndex := slices.IndexFunc(
			consumerConfigMap.OwnerReferences,
			func(ref metav1.OwnerReference) bool { return ptr.Deref(ref.Controller, false) },
		)
		var controllerRef *metav1.OwnerReference
		if controllerIndex != -1 {
			controllerRef = &consumerConfigMap.OwnerReferences[controllerIndex]
		}

		if controllerRef == nil || controllerRef.UID == r.storageConsumer.UID {
			if err := controllerutil.SetControllerReference(
				r.storageConsumer,
				consumerConfigMap,
				r.Scheme,
			); err != nil {
				return err
			}

			if consumerConfigMap.Data == nil {
				consumerConfigMap.Data = map[string]string{}
			}
			defaultConsumerResourceNames := util.GetStorageConsumerDefaultResourceNames(
				r.storageConsumer.Name,
				string(r.storageConsumer.UID),
				availableServices,
			)
			for key := range defaultConsumerResourceNames {
				consumerConfigMap.Data[key] = cmp.Or(
					strings.Trim(consumerConfigMap.Data[key], " "),
					defaultConsumerResourceNames[key],
				)
			}

			util.AddAnnotation(consumerConfigMap, primaryConsumerUIDAnnotation, string(r.storageConsumer.UID))
		} else {
			if err := controllerutil.SetOwnerReference(
				r.storageConsumer,
				consumerConfigMap,
				r.Scheme,
			); err != nil {
				return err
			}

			primaryRefIndex := slices.IndexFunc(
				consumerConfigMap.OwnerReferences,
				func(ref metav1.OwnerReference) bool {
					return string(ref.UID) == consumerConfigMap.GetAnnotations()[primaryConsumerUIDAnnotation]
				},
			)
			if primaryRefIndex == -1 {
				util.AddAnnotation(consumerConfigMap, primaryConsumerUIDAnnotation, string(r.storageConsumer.UID))
			}
		}
		return nil
	}); err != nil {
		return err
	}

	r.storageConsumer.Status.ResourceNameMappingConfigMap = corev1.LocalObjectReference{Name: consumerConfigMap.Name}
	return nil
}

func (r *StorageConsumerReconciler) reconcileCephRadosNamespace(
	radosNamespaceName string,
	additionalOwner client.Object,
	builtinBlockPools []string,
) error {
	blockPools := &rookCephv1.CephBlockPoolList{}
	if err := r.List(r.ctx, blockPools, client.InNamespace(r.namespace)); err != nil {
		return err
	}

	// ensure for this consumer a rados namespace is created in every blockpool
	var combinedErr error
	for idx := range blockPools.Items {
		bp := &blockPools.Items[idx]
		// a new radosnamespace is not required for internal pools
		if forInternalUseOnly, _ := strconv.ParseBool(bp.GetLabels()[util.ForInternalUseOnlyLabelKey]); forInternalUseOnly {
			continue
		}
		if slices.Contains(builtinBlockPools, bp.Name) {
			continue
		}

		// For erasure coded block pools creating rados namespaces is not supported
		if !reflect.DeepEqual(bp.Spec.ErasureCoded, rookCephv1.ErasureCodedSpec{}) {
			continue
		}

		rns := &rookCephv1.CephBlockPoolRadosNamespace{}
		rns.Name = fmt.Sprintf("%s-%s", bp.Name, radosNamespaceName)
		if radosNamespaceName == util.ImplicitRbdRadosNamespaceName {
			rns.Name = fmt.Sprintf(
				"%s-builtin-%s",
				bp.Name,
				util.ImplicitRbdRadosNamespaceName[1:len(util.ImplicitRbdRadosNamespaceName)-1],
			)
		}
		rns.Namespace = r.namespace

		shouldReconcile := true
		if !bp.DeletionTimestamp.IsZero() && bp.Status != nil {
			idx := slices.IndexFunc(bp.Status.Conditions, func(condition rookCephv1.Condition) bool {
				return condition.Type == rookCephv1.ConditionPoolDeletionIsBlocked
			})
			shouldReconcile = idx == -1 || bp.Status.Conditions[idx].Status != corev1.ConditionFalse
		}
		if shouldReconcile {
			if _, err := ctrl.CreateOrUpdate(r.ctx, r.Client, rns, func() error {
				if err := controllerutil.SetControllerReference(r.storageConsumer, rns, r.Scheme); err != nil {
					return err
				}
				if err := controllerutil.SetOwnerReference(additionalOwner, rns, r.Scheme); err != nil {
					return err
				}
				util.AddLabel(rns, blockPoolNameLabel, bp.Name)
				rns.Spec.Name = radosNamespaceName
				rns.Spec.BlockPoolName = bp.Name
				return nil
			}); err != nil {
				multierr.AppendInto(&combinedErr, err)
			}
		} else if err := r.Delete(r.ctx, rns); client.IgnoreNotFound(err) != nil {
			multierr.AppendInto(&combinedErr, err)
		}
	}

	return combinedErr
}

func (r *StorageConsumerReconciler) reconcileCephFilesystemSubVolumeGroup(
	cephFileSystemName string,
	subVolumeGroupName string,
	additionalOwner client.Object,
) error {
	cephFs := &rookCephv1.CephFilesystem{}
	cephFs.Name = cephFileSystemName
	cephFs.Namespace = r.namespace
	if err := r.get(cephFs); err != nil {
		return fmt.Errorf("failed to get CephFilesystem: %v", err)
	}

	svg := &rookCephv1.CephFilesystemSubVolumeGroup{}
	svg.Name = subVolumeGroupName
	svg.Namespace = r.namespace

	if _, err := ctrl.CreateOrUpdate(r.ctx, r.Client, svg, func() error {
		if err := controllerutil.SetControllerReference(r.storageConsumer, svg, r.Scheme); err != nil {
			return err
		}
		if err := controllerutil.SetOwnerReference(additionalOwner, svg, r.Scheme); err != nil {
			return err
		}
		svg.Spec.FilesystemName = cephFs.Name
		svg.Spec.Name = subVolumeGroupName
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (r *StorageConsumerReconciler) reconcileCephClientRBDProvisioner(
	cephClientName string,
	radosNamespaceName string,
	additionalOwner client.Object,
	csiCephUserGeneration int64,
) error {
	cephClient := &rookCephv1.CephClient{}
	cephClient.Name = cephClientName
	cephClient.Namespace = r.namespace
	if _, err := ctrl.CreateOrUpdate(r.ctx, r.Client, cephClient, func() error {
		if err := controllerutil.SetControllerReference(r.storageConsumer, cephClient, r.Scheme); err != nil {
			return err
		}
		if err := controllerutil.SetOwnerReference(additionalOwner, cephClient, r.Scheme); err != nil {
			return err
		}
		util.AddLabel(cephClient, util.CsiCephUserGenerationLabelKey, strconv.FormatInt(csiCephUserGeneration, 10))
		if radosNamespaceName == util.ImplicitRbdRadosNamespaceName {
			radosNamespaceName = "''"
		}
		cephClient.Spec.SecretName = cephClientName
		cephClient.Spec.Caps = map[string]string{
			"mon": "profile rbd, allow command 'osd blocklist'",
			"mgr": "allow rw",
			"osd": fmt.Sprintf("profile rbd namespace=%s", radosNamespaceName),
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (r *StorageConsumerReconciler) reconcileCephClientRBDNode(
	cephClientName string,
	radosNamespaceName string,
	additionalOwner client.Object,
	csiCephUserGeneration int64,
) error {
	cephClient := &rookCephv1.CephClient{}
	cephClient.Name = cephClientName
	cephClient.Namespace = r.namespace
	if _, err := ctrl.CreateOrUpdate(r.ctx, r.Client, cephClient, func() error {
		if err := controllerutil.SetControllerReference(r.storageConsumer, cephClient, r.Scheme); err != nil {
			return err
		}
		if err := controllerutil.SetOwnerReference(additionalOwner, cephClient, r.Scheme); err != nil {
			return err
		}
		util.AddLabel(cephClient, util.CsiCephUserGenerationLabelKey, strconv.FormatInt(csiCephUserGeneration, 10))
		if radosNamespaceName == util.ImplicitRbdRadosNamespaceName {
			radosNamespaceName = "''"
		}
		cephClient.Spec.SecretName = cephClientName
		cephClient.Spec.Caps = map[string]string{
			"mon": "profile rbd",
			"mgr": "allow rw",
			"osd": fmt.Sprintf("profile rbd namespace=%s", radosNamespaceName),
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (r *StorageConsumerReconciler) reconcileCephClientCephFSProvisioner(
	cephClientName string,
	cephFileSystemName string,
	subVolumeGroupName string,
	additionalOwner client.Object,
	csiCephUserGeneration int64,
) error {
	cephClient := &rookCephv1.CephClient{}
	cephClient.Name = cephClientName
	cephClient.Namespace = r.namespace
	if _, err := ctrl.CreateOrUpdate(r.ctx, r.Client, cephClient, func() error {
		if err := controllerutil.SetControllerReference(r.storageConsumer, cephClient, r.Scheme); err != nil {
			return err
		}
		if err := controllerutil.SetOwnerReference(additionalOwner, cephClient, r.Scheme); err != nil {
			return err
		}
		util.AddLabel(cephClient, util.CsiCephUserGenerationLabelKey, strconv.FormatInt(csiCephUserGeneration, 10))
		cephClient.Spec.SecretName = cephClientName
		cephClient.Spec.Caps = map[string]string{
			"mon": "allow r, allow command 'osd blocklist'",
			"mgr": "allow rw",
			"osd": fmt.Sprintf("allow rw tag cephfs metadata=%s", cephFileSystemName),
			"mds": fmt.Sprintf("allow rw path=/volumes/%s", subVolumeGroupName),
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (r *StorageConsumerReconciler) reconcileCephClientCephFSNode(
	cephClientName string,
	cephFileSystemName string,
	subVolumeGroupName string,
	additionalOwner client.Object,
	csiCephUserGeneration int64,
) error {
	cephClient := &rookCephv1.CephClient{}
	cephClient.Name = cephClientName
	cephClient.Namespace = r.namespace
	if _, err := ctrl.CreateOrUpdate(r.ctx, r.Client, cephClient, func() error {
		if err := controllerutil.SetControllerReference(r.storageConsumer, cephClient, r.Scheme); err != nil {
			return err
		}
		if err := controllerutil.SetOwnerReference(additionalOwner, cephClient, r.Scheme); err != nil {
			return err
		}
		util.AddLabel(cephClient, util.CsiCephUserGenerationLabelKey, strconv.FormatInt(csiCephUserGeneration, 10))
		cephClient.Spec.SecretName = cephClientName
		cephClient.Spec.Caps = map[string]string{
			"mon": "allow r",
			"mgr": "allow rw",
			"osd": fmt.Sprintf("allow rw tag cephfs *=%s", cephFileSystemName),
			"mds": fmt.Sprintf("allow rw path=/volumes/%s", subVolumeGroupName),
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (r *StorageConsumerReconciler) reconcileCephClientNfsProvisioner(
	cephClientName string,
	cephFileSystemName string,
	subVolumeGroupName string,
	additionalOwner client.Object,
	csiCephUserGeneration int64,
) error {
	cephClient := &rookCephv1.CephClient{}
	cephClient.Name = cephClientName
	cephClient.Namespace = r.namespace
	if _, err := ctrl.CreateOrUpdate(r.ctx, r.Client, cephClient, func() error {
		if err := controllerutil.SetControllerReference(r.storageConsumer, cephClient, r.Scheme); err != nil {
			return err
		}
		if err := controllerutil.SetOwnerReference(additionalOwner, cephClient, r.Scheme); err != nil {
			return err
		}
		util.AddLabel(cephClient, util.CsiCephUserGenerationLabelKey, strconv.FormatInt(csiCephUserGeneration, 10))
		cephClient.Spec.SecretName = cephClientName
		cephClient.Spec.Caps = map[string]string{
			"mon": "allow r, allow command 'osd blocklist'",
			"mgr": "allow rw",
			"osd": fmt.Sprintf("allow rw tag cephfs metadata=%s", cephFileSystemName),
			"mds": fmt.Sprintf("allow rw path=/volumes/%s", subVolumeGroupName),
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (r *StorageConsumerReconciler) reconcileCephClientNfsNode(
	cephClientName string,
	cephFileSystemName string,
	subVolumeGroupName string,
	additionalOwner client.Object,
	csiCephUserGeneration int64,
) error {
	cephClient := &rookCephv1.CephClient{}
	cephClient.Name = cephClientName
	cephClient.Namespace = r.namespace
	if _, err := ctrl.CreateOrUpdate(r.ctx, r.Client, cephClient, func() error {
		if err := controllerutil.SetControllerReference(r.storageConsumer, cephClient, r.Scheme); err != nil {
			return err
		}
		if err := controllerutil.SetOwnerReference(additionalOwner, cephClient, r.Scheme); err != nil {
			return err
		}
		util.AddLabel(cephClient, util.CsiCephUserGenerationLabelKey, strconv.FormatInt(csiCephUserGeneration, 10))
		cephClient.Spec.SecretName = cephClientName
		cephClient.Spec.Caps = map[string]string{
			"mon": "allow r",
			"mgr": "allow rw",
			"osd": fmt.Sprintf("allow rw tag cephfs *=%s", cephFileSystemName),
			"mds": fmt.Sprintf("allow rw path=/volumes/%s", subVolumeGroupName),
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (r *StorageConsumerReconciler) reconcileNoobaaAccount() error {
	noobaaAccount := &nbv1.NooBaaAccount{}
	noobaaAccount.Name = r.storageConsumer.Name
	noobaaAccount.Namespace = r.storageConsumer.Namespace
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, noobaaAccount, func() error {
		if err := r.own(noobaaAccount); err != nil {
			return err
		}
		// TODO: query the name of backing store during runtime
		noobaaAccount.Spec.DefaultResource = "noobaa-default-backing-store"
		// the following annotation will enable noobaa-operator to create a auth_token secret based on this account
		util.AddAnnotation(noobaaAccount, "remote-operator", "true")
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create noobaa account for storageConsumer %v: %v", r.storageConsumer.Name, err)
	}

	return nil
}

func (r *StorageConsumerReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.ctx, key, obj)
}

func (r *StorageConsumerReconciler) own(resource metav1.Object) error {
	// Ensure storageConsumer ownership on a resource
	return ctrl.SetControllerReference(r.storageConsumer, resource, r.Scheme)
}

// SetupWithManager sets up the controller with the Manager.
func (r *StorageConsumerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	enqueueForAllStorageConsumers := handler.EnqueueRequestsFromMapFunc(
		func(context context.Context, obj client.Object) []reconcile.Request {
			// Get the StorageConsumer objects
			consumers := &ocsv1alpha1.StorageConsumerList{}
			err := r.Client.List(context, consumers, &client.ListOptions{Namespace: obj.GetNamespace()})
			if err != nil {
				r.Log.Error(err, "Unable to list StorageConsumers")
				return []reconcile.Request{}
			}

			// Return name and namespace of the StorageClusters object
			request := make([]reconcile.Request, len(consumers.Items))
			for i := range consumers.Items {
				request[i] = reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(&consumers.Items[i]),
				}
			}

			return request
		},
	)

	statusDotClientChangedPredicate := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			objOld, oldOk := e.ObjectOld.(*ocsv1alpha1.StorageConsumer)
			objNew, newOk := e.ObjectNew.(*ocsv1alpha1.StorageConsumer)
			if !oldOk || !newOk {
				return false
			}
			oldClient := objOld.Status.Client
			newClient := objNew.Status.Client
			if oldClient == nil && newClient == nil {
				return false
			}
			return oldClient == nil && newClient != nil || oldClient != nil && newClient == nil || oldClient.ID != newClient.ID
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(
			&ocsv1alpha1.StorageConsumer{},
			builder.WithPredicates(
				predicate.Or(
					predicate.GenerationChangedPredicate{},
					predicate.AnnotationChangedPredicate{},
					statusDotClientChangedPredicate,
				),
			),
		).
		Owns(&nbv1.NooBaaAccount{}).
		Owns(&corev1.ConfigMap{}, builder.MatchEveryOwner).
		Owns(
			&rookCephv1.CephBlockPoolRadosNamespace{},
			builder.MatchEveryOwner,
			builder.WithPredicates(
				predicate.GenerationChangedPredicate{},
			),
		).
		Owns(
			&rookCephv1.CephFilesystemSubVolumeGroup{},
			builder.MatchEveryOwner,
			builder.WithPredicates(
				predicate.GenerationChangedPredicate{},
			),
		).
		Owns(&corev1.Secret{}).
		// Watch non-owned resources
		Watches(&rookCephv1.CephBlockPool{}, enqueueForAllStorageConsumers).
		Watches(&rookCephv1.CephFilesystem{}, enqueueForAllStorageConsumers).
		Watches(&rookCephv1.CephNFS{}, enqueueForAllStorageConsumers).
		Complete(r)
}
