package storagecluster

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	groupsnapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1beta1"
	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	subVolumeGroupName = "csi"
)

var (
	supportedCsiDrivers = []string{
		util.RbdDriverName,
		util.CephFSDriverName,
		util.NfsDriverName,
	}
)

type storageConsumer struct{}

var _ resourceManager = &storageConsumer{}

func (s *storageConsumer) ensureCreated(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (ctrl.Result, error) {

	storageClassesSpec, err := getLocalStorageClassNames(r.ctx, r.Client, storageCluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to generate storageclasses list for distribution: %v", err)
	}
	volumeSnapshotClassesSpec, err := getLocalVolumeSnapshotClassNames(r.ctx, r.Client, storageCluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to generate volumesnapshotclasses list for distribution: %v", err)
	}
	volumeGroupSnapshotClassesSpec, err := getLocalVolumeGroupSnapshotClassNames(r.ctx, r.Client, storageCluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to generate volumegroupsnapshotclasses list for distribution: %v", err)
	}

	networkFenceClassesSpec, err := getLocalNetworkFenceClassNames(r.ctx, r.Client, storageCluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to generate networkFenceClasses list for distribution: %v", err)
	}

	storageConsumer := &ocsv1a1.StorageConsumer{}
	storageConsumer.Name = defaults.LocalStorageConsumerName
	storageConsumer.Namespace = storageCluster.Namespace
	if _, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, storageConsumer, func() error {
		if err := controllerutil.SetControllerReference(storageCluster, storageConsumer, r.Scheme); err != nil {
			return err
		}
		spec := &storageConsumer.Spec
		spec.ResourceNameMappingConfigMap.Name = defaults.LocalStorageConsumerConfigMapName
		spec.StorageClasses = storageClassesSpec
		spec.VolumeSnapshotClasses = volumeSnapshotClassesSpec
		spec.NetworkFenceClasses = networkFenceClassesSpec
		// TODO: this is to support upgraded 4.18 provider mode cluster and should be retired in 4.20
		if dfVersion := storageConsumer.GetLabels()[util.CreatedAtDfVersionLabelKey]; dfVersion == "4.18" {
			for idx := range spec.VolumeSnapshotClasses {
				classSpec := &spec.VolumeSnapshotClasses[idx]
				switch classSpec.Name {
				case util.GenerateNameForSnapshotClass(storageCluster.Name, util.RbdSnapshotter):
					classSpec.Aliases = append(classSpec.Aliases, fmt.Sprintf("%s-ceph-rbd", storageCluster.Name))
				case util.GenerateNameForSnapshotClass(storageCluster.Name, util.CephfsSnapshotter):
					classSpec.Aliases = append(classSpec.Aliases, fmt.Sprintf("%s-cephfs", storageCluster.Name))
				}
			}
		}
		spec.VolumeGroupSnapshotClasses = volumeGroupSnapshotClassesSpec

		controllerutil.AddFinalizer(storageConsumer, internalComponentFinalizer)

		if val, ok := storageCluster.GetAnnotations()[defaults.KeyRotationEnableAnnotation]; ok {
			util.AddAnnotation(storageConsumer, defaults.KeyRotationEnableAnnotation, val)
		}

		if storageCluster.Spec.ManagedResources.CephNonResilientPools.Enable {
			// add a annotation to the storageconsumer that has the topology key for non resilient pools
			topologyKey := storageCluster.Status.FailureDomainKey
			util.AddAnnotation(storageConsumer, util.AnnotationNonResilientPoolsTopologyKey, topologyKey)
		} else {
			// remove the annotation if it exists
			delete(storageConsumer.GetAnnotations(), util.AnnotationNonResilientPoolsTopologyKey)
		}

		return nil
	}); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create/update storageconsumer %s: %v", storageConsumer.Name, err)
	}
	if storageConsumer.UID == "" {
		return ctrl.Result{}, fmt.Errorf("expected storageConsumer UID to not be empty")
	}

	availableServices, err := util.GetAvailableServices(r.ctx, r.Client, storageCluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get available services configured in StorageCluster: %v", err)
	}
	consumerConfigMap := &corev1.ConfigMap{}
	consumerConfigMap.Name = defaults.LocalStorageConsumerConfigMapName
	consumerConfigMap.Namespace = storageCluster.Namespace
	if _, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, consumerConfigMap, func() error {
		owners := consumerConfigMap.OwnerReferences
		controllerIndex := slices.IndexFunc(
			owners,
			func(ref metav1.OwnerReference) bool {
				return ref.UID != storageCluster.UID && ptr.Deref(ref.Controller, false)
			},
		)
		if controllerIndex != -1 {
			owners[controllerIndex].Controller = nil
		}

		if err := controllerutil.SetControllerReference(storageCluster, consumerConfigMap, r.Scheme); err != nil {
			return err
		}

		data := util.GetStorageConsumerDefaultResourceNames(
			defaults.LocalStorageConsumerName,
			string(storageConsumer.UID),
			availableServices,
		)
		resourceMap := util.WrapStorageConsumerResourceMap(data)
		if backwardCompatibleValue, exist := storageCluster.GetAnnotations()[util.BackwardCompatabilityInfoAnnotationKey]; !exist {
			resourceMap.ReplaceRbdRadosNamespaceName(util.ImplicitRbdRadosNamespaceName)
			resourceMap.ReplaceSubVolumeGroupName(subVolumeGroupName)
			resourceMap.ReplaceSubVolumeGroupRadosNamespaceName(subVolumeGroupName)
			resourceMap.ReplaceRbdClientProfileName("openshift-storage")
			resourceMap.ReplaceCephFsClientProfileName("openshift-storage")
			resourceMap.ReplaceNfsClientProfileName("openshift-storage")
			resourceMap.ReplaceCsiRbdNodeCephUserName("rook-csi-rbd-node")
			resourceMap.ReplaceCsiRbdProvisionerCephUserName("rook-csi-rbd-provisioner")
			resourceMap.ReplaceCsiCephFsNodeCephUserName("rook-csi-cephfs-node")
			resourceMap.ReplaceCsiCephFsProvisionerCephUserName("rook-csi-cephfs-provisioner")
			resourceMap.ReplaceCsiNfsNodeCephUserName("rook-csi-nfs-node")
			resourceMap.ReplaceCsiNfsProvisionerCephUserName("rook-csi-nfs-provisioner")
		} else {
			backwardCompatibleInfo := &util.BackwardCompatabilityInfo{}
			if err := json.Unmarshal([]byte(backwardCompatibleValue), backwardCompatibleInfo); err != nil {
				return fmt.Errorf("invalid backwardCompatibleInfo annotation, value is not a json: %v", err)
			}
			if backwardCompatibleInfo.Pre4_19InternalConsumer == "" {
				return fmt.Errorf("invalid backwardCompatibleInfo Pre4_19InternalConsumer is not set")
			}
			util.FillBackwardCompatibleConsumerConfigValues(storageCluster, backwardCompatibleInfo.Pre4_19InternalConsumer, resourceMap)
		}

		consumerConfigMap.Data = data
		return nil
	}); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create/update storageconsumer configmap %s: %v", consumerConfigMap.Name, err)
	}

	svgPre4_19 := &rookCephv1.CephFilesystemSubVolumeGroup{}
	svgPre4_19.Name = fmt.Sprintf("%s-%s", util.GenerateNameForCephFilesystem(storageCluster.Name), subVolumeGroupName)
	svgPre4_19.Namespace = storageCluster.Namespace
	// doing a get before delete ensures that we don't hit k8s server during every reconcile
	if err := r.Get(r.ctx, client.ObjectKeyFromObject(svgPre4_19), svgPre4_19); client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get pre4_19 subvolumegroup cr: %v", err)
	} else if svgPre4_19.UID != "" && svgPre4_19.DeletionTimestamp.IsZero() {
		if err := r.Delete(r.ctx, svgPre4_19); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to delete pre4_19 subvolumegroup cr: %v", err)
		}
	}

	return ctrl.Result{}, nil
}

func (s *storageConsumer) ensureDeleted(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (ctrl.Result, error) {
	storageConsumer := &ocsv1a1.StorageConsumer{}
	storageConsumer.Name = defaults.LocalStorageConsumerName
	storageConsumer.Namespace = storageCluster.Namespace
	if err := r.Get(r.ctx, client.ObjectKeyFromObject(storageConsumer), storageConsumer); client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get storageconsumer %s: %v", storageConsumer.Name, err)
	} else if storageConsumer.UID != "" {
		if storageConsumer.Status.Client != nil {
			return ctrl.Result{}, fmt.Errorf("waiting for client to offboard before deleting storageconsumer %s", storageConsumer.Name)
		}

		if err := r.Delete(r.ctx, storageConsumer); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to delete storageconsumer %s: %v", storageConsumer.Name, err)
		}

		if controllerutil.RemoveFinalizer(storageConsumer, internalComponentFinalizer) {
			r.Log.Info("Removing finalizer from StorageConsumer.", "StorageConsumer:", storageConsumer.Name, " StorageConsumer Namespace:", storageConsumer.Namespace, " Finalizer:", internalComponentFinalizer)
			if err := r.Update(r.ctx, storageConsumer); err != nil {
				r.Log.Info("Failed to remove finalizer from StorageConsumer.", "StorageConsumer:", storageConsumer.Name, "Finalizer:", internalComponentFinalizer)
				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer from StorageConsumer: %v", err)
			}
		}
	}

	// The internal consumer configmap is owned by both storagecluster and storage consumer and will not be deleted
	// before storage cluster is deleted. The ceph resources created by the consumer are owned by the primary consumer
	//  and consumer configmap. The storage cluster will wait in deletion phase till cephcluster is deleted, and the
	// ceph cluster will wait till all ceph resources are deleted. This creates a cyclic dependency, hence we need the
	// internal consumer configmap to be deleted when storagecluster deletion is triggered
	consumerConfigMap := &corev1.ConfigMap{}
	consumerConfigMap.Name = defaults.LocalStorageConsumerConfigMapName
	consumerConfigMap.Namespace = storageCluster.Namespace

	if err := r.Delete(r.ctx, consumerConfigMap); client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, fmt.Errorf("failed to delete local consumerConfigMap %s: %v", consumerConfigMap.Name, err)
	}

	return ctrl.Result{}, nil
}

func getExternalClassesBlaclistSelector() labels.Selector {
	blackListRequirement, err := labels.NewRequirement(
		util.ExternalClassLabelKey,
		selection.NotEquals,
		[]string{"true"},
	)
	if err != nil {
		panic(fmt.Sprintf("Error in external class label selector definition: %v", err))
	}

	return labels.NewSelector().Add(*blackListRequirement)
}

func getLocalStorageClassNames(ctx context.Context, kubeClient client.Client, storageCluster *ocsv1.StorageCluster) (
	[]ocsv1a1.StorageClassSpec, error) {

	storageClassNames := map[string]bool{}
	storageClassNames[util.GenerateNameForCephBlockPoolStorageClass(storageCluster)] = true
	storageClassNames[util.GenerateNameForCephFilesystemStorageClass(storageCluster)] = true

	crd := &metav1.PartialObjectMetadata{}
	crd.SetGroupVersionKind(extv1.SchemeGroupVersion.WithKind("CustomResourceDefinition"))
	crd.Name = VirtualMachineCrdName
	if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(crd), crd); client.IgnoreNotFound(err) != nil {
		return nil, err
	}
	if crd.UID != "" {
		storageClassNames[util.GenerateNameForCephBlockPoolVirtualizationStorageClass(storageCluster)] = true
	}

	if storageCluster.Spec.ManagedResources.CephNonResilientPools.Enable {
		storageClassNames[util.GenerateNameForNonResilientCephBlockPoolStorageClass(storageCluster)] = true
	}

	if storageCluster.Spec.NFS != nil && storageCluster.Spec.NFS.Enable {
		storageClassNames[util.GenerateNameForCephNetworkFilesystemStorageClass(storageCluster)] = true
	}

	if storageCluster.Spec.Encryption.StorageClass && storageCluster.Spec.Encryption.KeyManagementService.Enable {
		storageClassNames[util.GenerateNameForEncryptedCephBlockPoolStorageClass(storageCluster)] = true
	}

	// for day2 storageclasses
	storageClassesInCluster := &storagev1.StorageClassList{}
	if err := kubeClient.List(ctx, storageClassesInCluster, &client.MatchingLabelsSelector{
		// not select storageclass with external labels
		Selector: getExternalClassesBlaclistSelector(),
	}); err != nil {
		return nil, err
	}
	for idx := range storageClassesInCluster.Items {
		sc := &storageClassesInCluster.Items[idx]
		if slices.Contains(supportedCsiDrivers, sc.Provisioner) {
			storageClassNames[sc.Name] = true
		}
	}

	scSpec := make([]ocsv1a1.StorageClassSpec, 0, len(storageClassNames))
	for scName := range maps.Keys(storageClassNames) {
		// TODO: store the spec as the value which allows each value to be customizable corresponding to the name,
		// not doing now as we only have name in the storageclassspec
		scItem := ocsv1a1.StorageClassSpec{}
		scItem.Name = scName
		scSpec = append(scSpec, scItem)
	}
	return scSpec, nil
}

func getLocalVolumeSnapshotClassNames(ctx context.Context, kubeClient client.Client, storageCluster *ocsv1.StorageCluster) (
	[]ocsv1a1.VolumeSnapshotClassSpec, error) {

	volumeSnapshotClassNames := map[string]bool{}
	volumeSnapshotClassNames[util.GenerateNameForSnapshotClass(storageCluster.Name, util.RbdSnapshotter)] = true
	volumeSnapshotClassNames[util.GenerateNameForSnapshotClass(storageCluster.Name, util.CephfsSnapshotter)] = true

	if storageCluster.Spec.NFS != nil && storageCluster.Spec.NFS.Enable {
		volumeSnapshotClassNames[util.GenerateNameForSnapshotClass(storageCluster.Name, util.NfsSnapshotter)] = true
	}

	// for day2 volumesnapshotclasses
	volumeSnapshotClassesInCluster := &snapapi.VolumeSnapshotClassList{}
	if err := kubeClient.List(ctx, volumeSnapshotClassesInCluster, &client.MatchingLabelsSelector{
		// not select snapshotclasses with external labels
		Selector: getExternalClassesBlaclistSelector(),
	}); err != nil {
		return nil, err
	}
	for idx := range volumeSnapshotClassesInCluster.Items {
		// TODO: skip volumesnapshotclasses that are from external mode if both internal & external mode is enabled
		vsc := &volumeSnapshotClassesInCluster.Items[idx]
		if slices.Contains(supportedCsiDrivers, vsc.Driver) {
			volumeSnapshotClassNames[vsc.Name] = true
		}
	}

	vscSpec := make([]ocsv1a1.VolumeSnapshotClassSpec, 0, len(volumeSnapshotClassNames))
	for vscName := range maps.Keys(volumeSnapshotClassNames) {
		vscItem := ocsv1a1.VolumeSnapshotClassSpec{}
		vscItem.Name = vscName
		vscSpec = append(vscSpec, vscItem)
	}
	return vscSpec, nil
}

func getLocalVolumeGroupSnapshotClassNames(ctx context.Context, kubeClient client.Client, storageCluster *ocsv1.StorageCluster) (
	[]ocsv1a1.VolumeGroupSnapshotClassSpec, error) {

	volumeGroupSnapshotClassNames := map[string]bool{}
	volumeGroupSnapshotClassNames[util.GenerateNameForGroupSnapshotClass(storageCluster, util.RbdGroupSnapshotter)] = true
	volumeGroupSnapshotClassNames[util.GenerateNameForGroupSnapshotClass(storageCluster, util.CephfsGroupSnapshotter)] = true

	crd := &metav1.PartialObjectMetadata{}
	crd.SetGroupVersionKind(extv1.SchemeGroupVersion.WithKind("CustomResourceDefinition"))
	crd.Name = VolumeGroupSnapshotClassCrdName
	if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(crd), crd); client.IgnoreNotFound(err) != nil {
		return nil, err
	}
	if crd.UID != "" {
		// for day2 volumegroupsnapshotclasses
		volumeGroupSnapshotClassesInCluster := &groupsnapapi.VolumeGroupSnapshotClassList{}
		if err := kubeClient.List(ctx, volumeGroupSnapshotClassesInCluster, &client.MatchingLabelsSelector{
			// not select groupsnapshotclasses with external labels
			Selector: getExternalClassesBlaclistSelector(),
		}); err != nil {
			return nil, err
		}
		for idx := range volumeGroupSnapshotClassesInCluster.Items {
			// TODO: skip volumegroupsnapshotclasses that are from external mode if both internal & external mode is enabled
			vgsc := &volumeGroupSnapshotClassesInCluster.Items[idx]
			if slices.Contains(supportedCsiDrivers, vgsc.Driver) {
				volumeGroupSnapshotClassNames[vgsc.Name] = true
			}
		}
	}

	vgscSpec := make([]ocsv1a1.VolumeGroupSnapshotClassSpec, 0, len(volumeGroupSnapshotClassNames))
	for vgscName := range maps.Keys(volumeGroupSnapshotClassNames) {
		vgscItem := ocsv1a1.VolumeGroupSnapshotClassSpec{}
		vgscItem.Name = vgscName
		vgscSpec = append(vgscSpec, vgscItem)
	}
	return vgscSpec, nil
}

func getLocalNetworkFenceClassNames(ctx context.Context, kubeClient client.Client, storageCluster *ocsv1.StorageCluster) (
	[]ocsv1a1.NetworkFenceClassesSpec, error) {

	networkFenceClassNames := map[string]bool{}
	networkFenceClassNames[util.GenerateNameForNetworkFenceClass(storageCluster.Name, util.RbdNetworkFenceClass)] = true

	nfcSpec := make([]ocsv1a1.NetworkFenceClassesSpec, 0, len(networkFenceClassNames))
	for nfcName := range maps.Keys(networkFenceClassNames) {
		nfcItem := ocsv1a1.NetworkFenceClassesSpec{}
		nfcItem.Name = nfcName
		nfcSpec = append(nfcSpec, nfcItem)
	}
	return nfcSpec, nil
}
