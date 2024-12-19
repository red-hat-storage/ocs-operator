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
	"context"
	"fmt"
	"strings"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"

	"github.com/go-logr/logr"
	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	StorageConsumerNameLabel          = "ocs.openshift.io/storageconsumer-name"
	StorageRequestAnnotation          = "ocs.openshift.io.storagerequest"
	StorageCephUserTypeAnnotation     = "ocs.openshift.io.cephusertype"
	CephClientSecretNameAnnotationkey = "ocs.openshift.io/ceph-secret-name"
	ImplicitRnsAnnotationValue        = "-implicit"
	blockPoolNameLabel                = "ocs.openshift.io/cephblockpool-name"
	radosnamespaceAnnotationkey       = "ocs.openshift.io/radosnamespace"
	subvolumegroupAnnotationkey       = "ocs.openshift.io/subvolumegroup"
	discoveryAnnotationkey            = "ocs.openshift.io/storageconsumer-dependents"
	DefaultSubvolumeGroupName         = "csi"
)

// StorageConsumerReconciler reconciles a StorageConsumer object
type StorageConsumerReconciler struct {
	client.Client
	Log               logr.Logger
	Scheme            *runtime.Scheme
	OperatorNamespace string

	ctx                context.Context
	clusterId          *string
	storageClusterName string
	storageClusterUid  string
	isLocalConsumer    bool
	cephClientSuffix   string
	storageConsumer    *ocsv1alpha1.StorageConsumer
	noobaaAccount      *nbv1.NooBaaAccount

	cephRadosNamespace       *rookCephv1.CephBlockPoolRadosNamespace
	cephClientRbdProvisioner *rookCephv1.CephClient
	cephClientRbdNode        *rookCephv1.CephClient

	cephFilesystemSubVolumeGroup *rookCephv1.CephFilesystemSubVolumeGroup
	cephClientCephFsProvisioner  *rookCephv1.CephClient
	cephClientCephFsNode         *rookCephv1.CephClient
}

// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageconsumers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageconsumers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=noobaa.io,resources=noobaaaccounts,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storagerequests,verbs=get;list;watch
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storagerequests/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephclients,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephfilesystemsubvolumegroups,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephblockpoolradosnamespaces,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusters,verbs=get;watch;list
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;create;update

func (r *StorageConsumerReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {

	prevLogger := r.Log
	defer func() { r.Log = prevLogger }()
	r.Log = r.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	r.ctx = ctx

	r.storageConsumer = &ocsv1alpha1.StorageConsumer{}
	r.storageConsumer.Name = request.Name
	r.storageConsumer.Namespace = request.Namespace
	if err := r.get(r.storageConsumer); err != nil {
		if k8serrors.IsNotFound(err) {
			r.Log.Info("No StorageConsumer resource.")
			return reconcile.Result{}, nil
		}
		r.Log.Error(err, "Failed to retrieve StorageConsumer.")
		return reconcile.Result{}, err
	}

	result, reconcileError := r.reconcilePhases()
	statusError := r.Client.Status().Update(r.ctx, r.storageConsumer)
	if statusError != nil {
		r.Log.Info("Could not update StorageConsumer status.")
	}

	// Reconcile errors have higher priority than status update errors
	if reconcileError != nil {
		return result, reconcileError
	} else if statusError != nil {
		return result, statusError
	}

	return result, nil
}

func (r *StorageConsumerReconciler) discoverDependents() error {
	rnsList := &rookCephv1.CephBlockPoolRadosNamespaceList{}
	if err := r.List(
		r.ctx,
		rnsList,
		client.Limit(2),
		client.InNamespace(r.OperatorNamespace),
		client.MatchingLabels{StorageConsumerNameLabel: r.storageConsumer.Name},
	); err != nil {
		return err
	} else if len(rnsList.Items) > 1 {
		return fmt.Errorf("expected only a single radosnamespace for the storageconsumer: %v", r.storageConsumer.Name)
	} else if len(rnsList.Items) == 1 {
		// already onboarded client
		rns := &rnsList.Items[0]
		util.AddAnnotation(r.storageConsumer, blockPoolNameLabel, rns.GetLabels()[blockPoolNameLabel])
		util.AddAnnotation(r.storageConsumer, radosnamespaceAnnotationkey, rns.Name)
	} else if r.isLocalConsumer {
		// upgraded internal mode
		util.AddAnnotation(r.storageConsumer, radosnamespaceAnnotationkey, ImplicitRnsAnnotationValue)
	}

	svgList := &rookCephv1.CephFilesystemSubVolumeGroupList{}
	if err := r.List(
		r.ctx,
		svgList,
		client.Limit(2),
		client.InNamespace(r.OperatorNamespace),
		client.MatchingLabels{StorageConsumerNameLabel: r.storageConsumer.Name},
	); err != nil {
		return err
	} else if len(svgList.Items) > 1 {
		return fmt.Errorf("expected only a single subvolumegroup for the storageconsumer: %v", r.storageConsumer.Name)
	} else if len(svgList.Items) == 1 {
		// already onboarded client
		svg := svgList.Items[0]
		util.AddAnnotation(r.storageConsumer, subvolumegroupAnnotationkey, svg.Name)
	} else if r.isLocalConsumer {
		// upgraded internal mode
		util.AddAnnotation(r.storageConsumer, subvolumegroupAnnotationkey, DefaultSubvolumeGroupName)
	}

	util.AddAnnotation(r.storageConsumer, discoveryAnnotationkey, "done")
	return r.Update(r.ctx, r.storageConsumer)
}

func (r *StorageConsumerReconciler) reconcilePhases() (reconcile.Result, error) {
	if r.storageConsumer.GetDeletionTimestamp().IsZero() {
		if err := r.initPhase(); err != nil {
			return reconcile.Result{}, err
		}
		if err := r.reconcileCephRadosNamespace(); err != nil {
			return reconcile.Result{}, err
		}
		if err := r.reconcileCephFilesystemSubVolumeGroup(); err != nil {
			return reconcile.Result{}, err
		}
		if !r.isLocalConsumer {
			if err := r.reconcileNoobaaAccount(); err != nil {
				return reconcile.Result{}, err
			}
		}
		if err := r.consolidateStatus(); err != nil {
			return reconcile.Result{}, err
		}
	} else {
		r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateDeleting
	}
	return reconcile.Result{}, nil
}

func (r *StorageConsumerReconciler) initPhase() error {
	if !r.storageConsumer.Spec.Enable {
		r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateDisabled
		return nil
	}

	if r.clusterId == nil {
		clusterID := util.GetClusterID(r.ctx, r.Client, &r.Log)
		if clusterID == "" {
			return fmt.Errorf("failed to get clusterid")
		}
		r.clusterId = ptr.To(clusterID)
	}
	r.isLocalConsumer = strings.HasSuffix(r.storageConsumer.Name, *r.clusterId)

	storageClusters := &ocsv1.StorageClusterList{}
	if err := r.List(r.ctx, storageClusters, client.InNamespace(r.OperatorNamespace)); err != nil {
		return err
	}
	for idx := range storageClusters.Items {
		sc := &storageClusters.Items[idx]
		if !sc.Spec.ExternalStorage.Enable && sc.Status.Phase != util.PhaseIgnored {
			r.storageClusterName = sc.Name
			r.storageClusterUid = string(sc.UID)
			break
		}
	}
	if r.storageClusterName == "" || r.storageClusterUid == "" {
		return fmt.Errorf("failed to find storagecluster")
	}
	r.cephClientSuffix = strings.TrimPrefix(r.storageConsumer.Name, "storageconsumer-")

	// on success this will only be run once per consumer in it's lifetime
	if r.storageConsumer.GetAnnotations()[discoveryAnnotationkey] == "" {
		if err := r.discoverDependents(); err != nil {
			return err
		}
	}

	r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateConfiguring
	r.storageConsumer.Status.CephResources = []*ocsv1alpha1.CephResourcesSpec{}
	return nil
}

func (r *StorageConsumerReconciler) reconcileCephRadosNamespace() error {
	blockPools := &rookCephv1.CephBlockPoolList{}
	if err := r.List(r.ctx, blockPools, client.InNamespace(r.OperatorNamespace)); err != nil {
		return err
	}

	var existingRnsName string
	var createImplicitCaps bool
	var osdCaps strings.Builder
	// ensure for this consumer a rados namespace is created in every blockpool
	for idx := range blockPools.Items {
		bp := &blockPools.Items[idx]
		if bp.Name == "builtin-mgr" {
			continue
		}

		r.cephRadosNamespace = &rookCephv1.CephBlockPoolRadosNamespace{}
		r.cephRadosNamespace.Name = func() string {
			value := r.storageConsumer.GetAnnotations()[radosnamespaceAnnotationkey]
			if value == ImplicitRnsAnnotationValue {
				// implicit rns exist in every blockpool and this specific rns is only for tracking
				// and is not created via cr
				return fmt.Sprintf("%s%s", bp.Name, value)
			} else if value != "" && r.storageConsumer.GetAnnotations()[blockPoolNameLabel] == bp.Name {
				// in provider mode rns corresponding to this blockpool was created and we use the same name
				return value
			}
			// either this consumer doesn't has rns created in this specific blockpool or a new blockpool
			// is created and we need to ensure rns in that, the name of such rns is predictable
			return fmt.Sprintf("%s-%s", bp.Name, r.cephClientSuffix)
		}()
		r.cephRadosNamespace.Namespace = r.OperatorNamespace

		phase := ""
		if strings.HasSuffix(r.cephRadosNamespace.Name, ImplicitRnsAnnotationValue) {
			// implicit created rns and we just take the status of blockpool itself
			if bp.Status != nil {
				phase = string(bp.Status.Phase)
			}
			createImplicitCaps = true
		} else {
			// either rns was created in provider mode or new one needs to be created
			if !strings.HasSuffix(r.cephRadosNamespace.Name, r.cephClientSuffix) {
				// rns was created in provider mode
				existingRnsName = r.cephRadosNamespace.Name
			}
			if _, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephRadosNamespace, func() error {
				// if rns was created in provider mode storagerequest will be the controller owner
				// as only single controller owner is permitted remove the old and place consumer as the owner
				if existing := metav1.GetControllerOfNoCopy(r.cephRadosNamespace); existing != nil &&
					existing.Kind != r.storageConsumer.Kind {
					existing.BlockOwnerDeletion = nil
					existing.Controller = nil
				}
				if err := r.own(r.cephRadosNamespace); err != nil {
					return err
				}

				if strings.HasSuffix(r.cephRadosNamespace.Name, r.cephClientSuffix) {
					// for each new rns the name is just the consumer name across all block pools
					// setting this just overrides the name of cr which is used as rns name to be created in blockpool
					r.cephRadosNamespace.Spec.Name = r.storageConsumer.Name
				}
				r.cephRadosNamespace.Spec.BlockPoolName = bp.Name
				return nil
			}); err != nil {
				return err
			}
			if r.cephRadosNamespace.Status != nil {
				phase = string(r.cephRadosNamespace.Status.Phase)
			}
		}
		r.setCephResourceStatus(r.cephRadosNamespace.Name, "CephBlockPoolRadosNamespace", phase)
	}
	if createImplicitCaps {
		// TODO: change from rook created client is the addition of [namespace '']
		// allows access to implicit namespaces on all blockpools
		osdCaps.WriteString("profile rbd namespace ''")
	} else {
		if existingRnsName != "" {
			// allows access to the radosnamespace which was created in provider mode
			osdCaps.WriteString(fmt.Sprintf("profile rbd namespace=%s, ", existingRnsName))
		}
		// it could happen this consumer currently uses rns created in provider mode but adding
		// caps for rns with same name as storageconsumer gives it's access to future rns's as well
		// without changing keyrings

		// allows access to a specific radosnamespace across all blockpools
		osdCaps.WriteString(fmt.Sprintf("profile rbd namespace=%s", r.storageConsumer.Name))
	}

	r.cephClientRbdProvisioner = &rookCephv1.CephClient{}
	r.cephClientRbdProvisioner.Name = generateClientName("rbd", "provisioner", r.cephClientSuffix)
	r.cephClientRbdProvisioner.Namespace = r.OperatorNamespace
	if _, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephClientRbdProvisioner, func() error {
		if err := r.own(r.cephClientRbdProvisioner); err != nil {
			return err
		}
		// storagecluster uid is being used as it is available even before client onboard
		// and storageclasses created by other controllers can have the secret named properly
		// ensuring secret name uniqueness can be removed if there'll be no changes in storageclasses in the future
		util.AddAnnotation(
			r.cephClientRbdProvisioner,
			CephClientSecretNameAnnotationkey,
			util.GenerateCephClientSecretName("rbd", "provisioner", r.storageClusterUid),
		)
		r.cephClientRbdProvisioner.Spec = rookCephv1.ClientSpec{
			Caps: map[string]string{
				"mon": "profile rbd, allow command 'osd blocklist'",
				"mgr": "allow rw",
				"osd": osdCaps.String(),
			},
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to create/update cephclient for rbd provisioner: %v", err)
	}
	phase := ""
	if r.cephClientRbdProvisioner.Status != nil {
		phase = string(r.cephClientRbdProvisioner.Status.Phase)
	}
	r.setCephResourceStatus(r.cephClientRbdProvisioner.Name, "CephClient", phase)
	if createImplicitCaps && phase == util.PhaseReady {
		// TODO: this is required for metrics exporter and if rook continues to
		// create default clients then this block will be removed
		csiSecret := &corev1.Secret{}
		csiSecret.Name = "rook-csi-rbd-provisioner"
		csiSecret.Namespace = r.OperatorNamespace
		if err := r.get(csiSecret); client.IgnoreNotFound(err) != nil {
			return err
		} else if csiSecret.UID == "" {
			existingSecret := &corev1.Secret{}
			existingSecret.Name = r.cephClientRbdProvisioner.Status.Info["secretName"]
			existingSecret.Namespace = r.OperatorNamespace
			if existingSecret.Name == "" {
				return fmt.Errorf("secret name not found in cephclient status")
			}
			if err := r.own(csiSecret); err != nil {
				return err
			}
			csiSecret.Data = existingSecret.Data
			if err := r.Create(r.ctx, csiSecret); err != nil {
				return err
			}
		}
	}

	r.cephClientRbdNode = &rookCephv1.CephClient{}
	r.cephClientRbdNode.Name = generateClientName("rbd", "node", r.cephClientSuffix)
	r.cephClientRbdNode.Namespace = r.OperatorNamespace
	if _, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephClientRbdNode, func() error {
		if err := r.own(r.cephClientRbdNode); err != nil {
			return err
		}
		util.AddAnnotation(
			r.cephClientRbdNode,
			CephClientSecretNameAnnotationkey,
			util.GenerateCephClientSecretName("rbd", "node", r.storageClusterUid),
		)
		r.cephClientRbdNode.Spec = rookCephv1.ClientSpec{
			Caps: map[string]string{
				"mon": "profile rbd",
				"mgr": "allow rw",
				"osd": osdCaps.String(),
			},
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to create/update cephclient for rbd node: %v", err)
	}
	phase = ""
	if r.cephClientRbdNode.Status != nil {
		phase = string(r.cephClientRbdNode.Status.Phase)
	}
	r.setCephResourceStatus(r.cephClientRbdNode.Name, "CephClient", phase)

	return nil
}

func (r *StorageConsumerReconciler) reconcileCephFilesystemSubVolumeGroup() error {
	cephFs := &rookCephv1.CephFilesystem{}
	cephFs.Name = fmt.Sprintf("%s-%s", r.storageClusterName, "cephfilesystem")
	cephFs.Namespace = r.OperatorNamespace
	if err := r.get(cephFs); err != nil {
		return fmt.Errorf("failed to get CephFilesystem: %v", err)
	}

	var defaultSvgExist bool
	var existingSvgName string
	var mdsCaps strings.Builder
	r.cephFilesystemSubVolumeGroup = &rookCephv1.CephFilesystemSubVolumeGroup{}
	r.cephFilesystemSubVolumeGroup.Name = func() string {
		value := r.storageConsumer.GetAnnotations()[subvolumegroupAnnotationkey]
		if value == DefaultSubvolumeGroupName {
			// handled by storagecluster controller(s)
			return fmt.Sprintf("%s-%s", cephFs.Name, value)
		} else if value != "" {
			// in provider mode svg was already created and we use the same name
			return value
		}
		// new svg gets the names from the consumer
		return fmt.Sprintf("%s-%s", cephFs.Name, r.cephClientSuffix)
	}()
	r.cephFilesystemSubVolumeGroup.Namespace = r.OperatorNamespace
	if strings.HasSuffix(r.cephFilesystemSubVolumeGroup.Name, DefaultSubvolumeGroupName) {
		defaultSvgExist = true
		// we need status of svg
		if err := r.get(r.cephFilesystemSubVolumeGroup); err != nil {
			return err
		}
	} else {
		if !strings.HasSuffix(r.cephFilesystemSubVolumeGroup.Name, r.cephClientSuffix) {
			// svg was created in provider mode
			existingSvgName = r.cephFilesystemSubVolumeGroup.Name
		}
		if _, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephFilesystemSubVolumeGroup, func() error {
			// if svg was created in provider mode storagerequest will be the controller owner
			// as only single controller owner is permitted remove the old and place consumer as the owner
			if existing := metav1.GetControllerOfNoCopy(r.cephFilesystemSubVolumeGroup); existing != nil &&
				existing.Kind != r.storageConsumer.Kind {
				existing.BlockOwnerDeletion = nil
				existing.Controller = nil
			}
			if err := r.own(r.cephFilesystemSubVolumeGroup); err != nil {
				return err
			}
			// we are not tracking the data pool as it was same in both internal and provider mode
			r.cephFilesystemSubVolumeGroup.Spec.FilesystemName = cephFs.Name
			return nil
		}); err != nil {
			return err
		}
	}
	phase := ""
	if r.cephFilesystemSubVolumeGroup.Status != nil {
		phase = string(r.cephFilesystemSubVolumeGroup.Status.Phase)
	}
	r.setCephResourceStatus(r.cephFilesystemSubVolumeGroup.Name, "CephFilesystemSubVolumeGroup", phase)
	if defaultSvgExist {
		// TODO: change from rook created client is the addition of [path=/volume/<svg>]
		// allows access to default subvolumegroup
		mdsCaps.WriteString(fmt.Sprintf("allow * path=/volumes/%s", DefaultSubvolumeGroupName))
	} else {
		if existingSvgName != "" {
			// allows access to svg which was created in provider mode
			mdsCaps.WriteString(fmt.Sprintf("allow rw path=/volumes/%s, ", existingSvgName))
		}
		// it could happen this consumer currently uses svg created in provider mode but adding
		// caps for svg with same name as storageconsumer gives it's access to future svg's as well
		// without changing keyrings

		// allows access to a specific svg
		mdsCaps.WriteString(fmt.Sprintf("allow rw path=/volumes/%s", r.storageConsumer.Name))
	}

	r.cephClientCephFsProvisioner = &rookCephv1.CephClient{}
	r.cephClientCephFsProvisioner.Name = generateClientName("cephfs", "provisioner", r.cephClientSuffix)
	r.cephClientCephFsProvisioner.Namespace = r.OperatorNamespace
	if _, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephClientCephFsProvisioner, func() error {
		if err := r.own(r.cephClientCephFsProvisioner); err != nil {
			return err
		}
		util.AddAnnotation(
			r.cephClientCephFsProvisioner,
			CephClientSecretNameAnnotationkey,
			util.GenerateCephClientSecretName("cephfs", "provisioner", r.storageClusterUid),
		)
		r.cephClientCephFsProvisioner.Spec = rookCephv1.ClientSpec{
			Caps: map[string]string{
				"mon": "allow r, allow command 'osd blocklist'",
				"mgr": "allow rw",
				"osd": "allow rw tag cephfs metadata=*",
				"mds": mdsCaps.String(),
			},
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to create/update cephclient for cephfs provisioner: %v", err)
	}
	phase = ""
	if r.cephClientCephFsProvisioner.Status != nil {
		phase = string(r.cephClientCephFsProvisioner.Status.Phase)
	}
	r.setCephResourceStatus(r.cephClientCephFsProvisioner.Name, "CephClient", phase)

	r.cephClientCephFsNode = &rookCephv1.CephClient{}
	r.cephClientCephFsNode.Name = generateClientName("cephfs", "node", r.cephClientSuffix)
	r.cephClientCephFsNode.Namespace = r.OperatorNamespace
	if _, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephClientCephFsNode, func() error {
		if err := r.own(r.cephClientCephFsNode); err != nil {
			return err
		}
		util.AddAnnotation(
			r.cephClientCephFsNode,
			CephClientSecretNameAnnotationkey,
			util.GenerateCephClientSecretName("cephfs", "node", r.storageClusterUid),
		)
		r.cephClientCephFsNode.Spec = rookCephv1.ClientSpec{
			Caps: map[string]string{
				"mon": "allow r",
				"mgr": "allow rw",
				"osd": "allow rw tag cephfs metadata=*",
				"mds": mdsCaps.String(),
			},
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to create/update cephclient for cephfs node: %v", err)
	}
	phase = ""
	if r.cephClientCephFsNode.Status != nil {
		phase = string(r.cephClientCephFsNode.Status.Phase)
	}
	r.setCephResourceStatus(r.cephClientCephFsNode.Name, "CephClient", phase)

	return nil
}

func (r *StorageConsumerReconciler) reconcileNoobaaAccount() error {
	r.noobaaAccount = &nbv1.NooBaaAccount{}
	r.noobaaAccount.Name = r.storageConsumer.Name
	r.noobaaAccount.Namespace = r.OperatorNamespace
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.noobaaAccount, func() error {
		if err := r.own(r.noobaaAccount); err != nil {
			return err
		}
		// TODO: query the name of backing store during runtime
		r.noobaaAccount.Spec.DefaultResource = "noobaa-default-backing-store"
		// the following annotation will enable noobaa-operator to create a auth_token secret based on this account
		util.AddAnnotation(r.noobaaAccount, "remote-operator", "true")
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create noobaa account for storageConsumer %v: %v", r.storageConsumer.Name, err)
	}

	phase := string(r.noobaaAccount.Status.Phase)
	r.setCephResourceStatus(r.noobaaAccount.Name, "NooBaaAccount", phase)

	return nil
}

func (r *StorageConsumerReconciler) consolidateStatus() error {
	cephResourcesReady := true
	for _, cephResource := range r.storageConsumer.Status.CephResources {
		if cephResource.Phase != "Ready" {
			cephResourcesReady = false
			break
		}
	}
	if cephResourcesReady {
		r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateReady
	}
	return nil
}

func (r *StorageConsumerReconciler) setCephResourceStatus(name string, kind string, phase string) {
	cephResourceSpec := &ocsv1alpha1.CephResourcesSpec{
		Name:  name,
		Kind:  kind,
		Phase: phase,
	}
	r.storageConsumer.Status.CephResources = append(
		r.storageConsumer.Status.CephResources,
		cephResourceSpec,
	)
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
	enqueueStorageConsumer := handler.EnqueueRequestsFromMapFunc(
		func(ctx context.Context, obj client.Object) []reconcile.Request {
			scList := &ocsv1alpha1.StorageConsumerList{}
			if err := r.Client.List(ctx, scList, client.InNamespace(obj.GetNamespace())); err != nil {
				return nil
			}
			requests := make([]reconcile.Request, len(scList.Items))
			for idx := range requests {
				requests[idx] = reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(&scList.Items[idx]),
				}
			}
			return requests
		},
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&ocsv1alpha1.StorageConsumer{}, builder.WithPredicates(
			predicate.GenerationChangedPredicate{},
		)).
		Owns(&nbv1.NooBaaAccount{}).
		Owns(&rookCephv1.CephBlockPoolRadosNamespace{}).
		Owns(&rookCephv1.CephFilesystemSubVolumeGroup{}).
		Owns(&rookCephv1.CephClient{}).
		Watches(&rookCephv1.CephBlockPool{}, enqueueStorageConsumer).
		Watches(&rookCephv1.CephFilesystem{}, enqueueStorageConsumer).
		Complete(r)
}

func generateClientName(storageType, userType, consumerName string) string {
	return fmt.Sprintf("%s-%s-%s", storageType, userType, consumerName)
}
