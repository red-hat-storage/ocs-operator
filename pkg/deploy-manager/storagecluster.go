package deploymanager

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8sv1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// jsonPatch for removing finalizers
	finalizerRemovalPatch = `[{ "op": "replace", "path": "/metadata/finalizers", "value":null}]`
)

// StartDefaultStorageCluster creates and waits on a StorageCluster to come online
func (t *DeployManager) StartDefaultStorageCluster() error {
	// create the namespaces we'll work with
	err := t.CreateNamespace(InstallNamespace)
	if err != nil {
		return err
	}

	// label the worker nodes with the storage label
	err = t.labelWorkerNodes()
	if err != nil {
		return err
	}

	// start the storage cluster
	err = t.startStorageCluster()
	if err != nil {
		return err
	}

	// Ensure storage cluster is online before starting tests
	err = t.WaitOnStorageCluster()
	if err != nil {
		return err
	}

	return nil
}

// DefaultStorageCluster returns a default StorageCluster manifest
func (t *DeployManager) DefaultStorageCluster() (*ocsv1.StorageCluster, error) {
	arbiter := ocsv1.ArbiterSpec{}
	nodeTopologies := &ocsv1.NodeTopologyMap{}
	if t.ArbiterEnabled() {
		arbiter.Enable = true
		nodeTopologies.ArbiterLocation = t.GetArbiterZone()
	}

	monQuantity, err := resource.ParseQuantity("10Gi")
	if err != nil {
		return nil, err
	}
	dataQuantity, err := resource.ParseQuantity("100Gi")
	if err != nil {
		return nil, err
	}
	storageClassName := "gp2-csi"
	blockVolumeMode := k8sv1.PersistentVolumeBlock
	storageCluster := &ocsv1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultStorageClusterName,
			Namespace: InstallNamespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					Name:       DefaultStorageClusterStorageSystemName,
					Kind:       "StorageSystem",
					APIVersion: "v1",
					UID:        types.UID(DefaultStorageClusterStorageSystemName),
				},
			},
		},
		Spec: ocsv1.StorageClusterSpec{
			ManageNodes: false,
			MonPVCTemplate: &k8sv1.PersistentVolumeClaim{
				Spec: k8sv1.PersistentVolumeClaimSpec{
					StorageClassName: &storageClassName,
					AccessModes:      []k8sv1.PersistentVolumeAccessMode{k8sv1.ReadWriteOnce},

					Resources: k8sv1.VolumeResourceRequirements{
						Requests: k8sv1.ResourceList{
							"storage": monQuantity,
						},
					},
				},
			},
			NFS: &ocsv1.NFSSpec{
				Enable: true,
			},
			// Setting empty ResourceLists to prevent ocs-operator from setting the
			// default resource requirements
			Resources: map[string]corev1.ResourceRequirements{
				"mon": {
					Requests: corev1.ResourceList{},
					Limits:   corev1.ResourceList{},
				},
				"mds": {
					Requests: corev1.ResourceList{},
					Limits:   corev1.ResourceList{},
				},
				"nfs": {
					Requests: corev1.ResourceList{},
					Limits:   corev1.ResourceList{},
				},
				"rgw": {
					Requests: corev1.ResourceList{},
					Limits:   corev1.ResourceList{},
				},
				"mgr": {
					Requests: corev1.ResourceList{},
					Limits:   corev1.ResourceList{},
				},
				"noobaa-core": {
					Requests: corev1.ResourceList{},
					Limits:   corev1.ResourceList{},
				},
				"noobaa-db": {
					Requests: corev1.ResourceList{},
					Limits:   corev1.ResourceList{},
				},
				"noobaa-endpoint": {
					Requests: corev1.ResourceList{},
					Limits:   corev1.ResourceList{},
				},
			},
			StorageDeviceSets: []ocsv1.StorageDeviceSet{
				{
					Name:     "example-deviceset",
					Count:    1,
					Replica:  t.getMinOSDsCount(),
					Portable: true,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},

					DataPVCTemplate: k8sv1.PersistentVolumeClaim{
						ObjectMeta: metav1.ObjectMeta{
							Name: "data",
						},
						Spec: k8sv1.PersistentVolumeClaimSpec{
							StorageClassName: &storageClassName,
							AccessModes:      []k8sv1.PersistentVolumeAccessMode{k8sv1.ReadWriteOnce},
							VolumeMode:       &blockVolumeMode,

							Resources: k8sv1.VolumeResourceRequirements{
								Requests: k8sv1.ResourceList{
									"storage": dataQuantity,
								},
							},
						},
					},
				},
			},
			NodeTopologies: nodeTopologies,
			Arbiter:        arbiter,
		},
	}
	storageCluster.SetGroupVersionKind(schema.GroupVersionKind{Group: ocsv1.GroupVersion.Group, Kind: "StorageCluster", Version: ocsv1.GroupVersion.Version})

	return storageCluster, nil
}

// getStorageCluster retrieves the test suite storage cluster
func (t *DeployManager) getStorageCluster() (*ocsv1.StorageCluster, error) {
	sc := &ocsv1.StorageCluster{}
	err := t.Client.Get(context.TODO(), types.NamespacedName{Name: DefaultStorageClusterName, Namespace: InstallNamespace}, sc)

	if err != nil {
		return nil, err
	}

	return sc, nil
}

// createStorageCluster is used to install the test suite storage cluster
func (t *DeployManager) createStorageCluster() error {
	sc, err := t.DefaultStorageCluster()
	if err != nil {
		return err
	}

	err = t.Client.Create(context.TODO(), sc)

	if err != nil {
		return err
	}

	return nil
}

// deleteStorageCluster is used to delete the test suite storage cluster
func (t *DeployManager) deleteStorageCluster() error {
	sc, err := t.DefaultStorageCluster()
	if err != nil {
		return err
	}

	err = t.Client.Delete(context.TODO(), sc)
	return err
}

// WaitOnStorageCluster waits for storage cluster to come online
func (t *DeployManager) WaitOnStorageCluster() error {
	timeout := 1200 * time.Second
	// NOTE the long timeout above. It can take quite a bit of time for this
	// storage cluster to fully initialize
	interval := 10 * time.Second
	lastReason := ""
	ctx := context.TODO()

	err := utilwait.PollUntilContextTimeout(ctx, interval, timeout, true, func(context.Context) (done bool, err error) {
		sc, err := t.getStorageCluster()
		if err != nil {
			lastReason = fmt.Sprintf("%v", err)
			return false, nil
		}

		_true := true
		_false := false
		var available *bool
		var upgradeable *bool
		var progressing *bool
		var degraded *bool

		for _, condition := range sc.Status.Conditions {
			switch condition.Type {
			case conditionsv1.ConditionAvailable:
				available = &_false
				if condition.Status == k8sv1.ConditionTrue {
					available = &_true
				}
			case conditionsv1.ConditionProgressing:
				progressing = &_false
				if condition.Status == k8sv1.ConditionTrue {
					progressing = &_true
				}
			case conditionsv1.ConditionDegraded:
				degraded = &_false
				if condition.Status == k8sv1.ConditionTrue {
					degraded = &_true
				}
			case conditionsv1.ConditionUpgradeable:
				upgradeable = &_false
				if condition.Status == k8sv1.ConditionTrue {
					upgradeable = &_true
				}
			}
		}

		// we have to wait for all of these conditions to exist as well as be set
		if available == nil {
			lastReason = fmt.Sprintf("Waiting on 'available' condition to be set") //nolint:gosimple
			return false, nil
		} else if upgradeable == nil {
			lastReason = fmt.Sprintf("Waiting on 'upgradeable' condition to be set") //nolint:gosimple
			return false, nil
		} else if progressing == nil {
			lastReason = fmt.Sprintf("Waiting on 'progressing' condition to be set") //nolint:gosimple
			return false, nil
		} else if degraded == nil {
			lastReason = fmt.Sprintf("Waiting on 'degraded' condition to be set") //nolint:gosimple
			return false, nil
		}

		if !*available || !*upgradeable || *degraded || *progressing {
			lastReason = fmt.Sprintf("waiting on storage cluster to come online. available: %t, upgradeable: %t, progressing: %t, degraded: %t",
				*available,
				*upgradeable,
				*progressing,
				*degraded)
			return false, nil
		}

		// We expect at least 3 osd deployments to be online and available
		deployments := &appsv1.DeploymentList{}
		err = t.Client.List(context.TODO(), deployments, &client.ListOptions{
			Namespace:     InstallNamespace,
			LabelSelector: labels.SelectorFromSet(map[string]string{"app": "rook-ceph-osd"}),
		})
		if err != nil {
			lastReason = fmt.Sprintf("%v", err)
			return false, nil
		}
		osdsOnline := 0
		for _, deployment := range deployments.Items {
			expectedReplicas := int32(1)
			if deployment.Spec.Replicas != nil {
				expectedReplicas = *deployment.Spec.Replicas
			}

			if expectedReplicas == deployment.Status.ReadyReplicas {
				osdsOnline++
			}
		}

		if osdsOnline < t.getMinOSDsCount() {
			lastReason = fmt.Sprintf("%d/%d expected OSDs are online", osdsOnline, t.getMinOSDsCount())
		}

		// expect noobaa-core pod with label selector (noobaa-core=noobaa) to be running
		pods := &k8sv1.PodList{}
		err = t.Client.List(context.TODO(), pods, &client.ListOptions{
			Namespace:     InstallNamespace,
			LabelSelector: labels.SelectorFromSet(map[string]string{"noobaa-core": "noobaa"}),
		})
		if err != nil {
			lastReason = fmt.Sprintf("%v", err)
			return false, nil
		}

		noobaaCoreOnline := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == k8sv1.PodRunning {
				noobaaCoreOnline++
			}
		}

		if noobaaCoreOnline == 0 {
			lastReason = "Waiting on noobaa-core pod to come online"
			return false, nil
		}

		return true, nil

	})

	if err != nil {
		return fmt.Errorf("%v: %s", err, lastReason)
	}
	return nil
}

func (t *DeployManager) labelWorkerNodes() error {
	labeledCount := 0
	arbiterNodeCount := 0
	var arbiterZone string

	if t.ArbiterEnabled() {
		err := t.electArbiterZone()
		if err != nil {
			return err
		}
		arbiterZone = t.GetArbiterZone()
		if arbiterZone == "" {
			return fmt.Errorf("Arbiter zone is not set")
		}
	}

	nodes := &k8sv1.NodeList{}
	err := t.Client.List(context.TODO(), nodes, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{"node-role.kubernetes.io/worker": ""}),
	})
	if err != nil {
		return err
	}

	for _, node := range nodes.Items {
		currentNode, err := json.Marshal(node)
		if err != nil {
			return err
		}
		updatedNode := node.DeepCopy()

		if t.ArbiterEnabled() && node.GetLabels()[corev1.LabelZoneFailureDomainStable] == arbiterZone {
			// don't label the nodes in the arbiter zone
			arbiterNodeCount++
			continue
		}
		updatedNode.Labels["cluster.ocs.openshift.io/openshift-storage"] = ""

		newJSON, err := json.Marshal(updatedNode)
		if err != nil {
			return err
		}

		patch, err := strategicpatch.CreateTwoWayMergePatch(currentNode, newJSON, node)
		if err != nil {
			return err
		}

		err = t.Client.Patch(context.TODO(), updatedNode, client.RawPatch(types.StrategicMergePatchType, patch))
		if err != nil {
			return err
		}

		labeledCount++
	}

	if labeledCount < t.getMinOSDsCount() {
		return fmt.Errorf("Only %d worker nodes found when we need %d to deploy", labeledCount, t.getMinOSDsCount())
	}
	if t.ArbiterEnabled() && arbiterNodeCount < 1 {
		return fmt.Errorf("No arbiter nodes found, we need at least 1")
	}

	return nil
}

func (t *DeployManager) startStorageCluster() error {
	// Ensure storage cluster is created
	err := t.createStorageCluster()
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (t *DeployManager) DeleteStorageCluster() error {
	// Delete storage cluster and wait for it to be deleted
	err := t.DeleteStorageClusterAndWait(InstallNamespace)
	if err != nil {
		return err
	}
	return nil
}

func (t *DeployManager) GetStorageClasses() (map[string]bool, error) {
	// Get the list of storage classes
	SCList := &storagev1.StorageClassList{}
	err := t.Client.List(context.TODO(), SCList)
	if err != nil {
		return nil, err
	}

	SCNames := map[string]bool{}
	for _, sc := range SCList.Items {
		SCNames[sc.Name] = true
	}

	return SCNames, nil
}

func (t *DeployManager) AddCustomStorageClassName(customSCNames map[string]string) error {
	sc, err := t.getStorageCluster()
	if err != nil {
		return err
	}

	sc.Spec.ManagedResources = ocsv1.ManagedResourcesSpec{
		CephBlockPools: ocsv1.ManageCephBlockPools{
			StorageClassName: customSCNames["CephBlockPools"],
		},
		CephFilesystems: ocsv1.ManageCephFilesystems{
			StorageClassName: customSCNames["CephFilesystems"],
		},
	}

	if sc.Spec.ManagedResources.CephNonResilientPools.Enable {
		sc.Spec.ManagedResources = ocsv1.ManagedResourcesSpec{
			CephNonResilientPools: ocsv1.ManageCephNonResilientPools{
				StorageClassName: customSCNames["CephNonResilientPools"],
			},
		}
	}

	if sc.Spec.NFS.Enable {
		sc.Spec.NFS = &ocsv1.NFSSpec{
			StorageClassName: customSCNames["NFS"],
		}
	}

	if sc.Spec.Encryption.StorageClass && sc.Spec.Encryption.KeyManagementService.Enable {
		sc.Spec.Encryption = ocsv1.EncryptionSpec{
			StorageClassName: customSCNames["Encryption"],
		}
	}

	err = t.Client.Update(context.TODO(), sc)
	return err
}

func (t *DeployManager) VerifyStorageClassesExist(oldSC map[string]bool) (bool, error) {
	currentSC, err := t.GetStorageClasses()
	if err != nil {
		return false, err
	}

	expectedSC := oldSC

	sc, err := t.getStorageCluster()
	if err != nil {
		return false, err
	}

	if sc.Spec.ManagedResources.CephBlockPools.StorageClassName != "" {
		expectedSC[sc.Spec.ManagedResources.CephBlockPools.StorageClassName] = true
	}
	if sc.Spec.ManagedResources.CephFilesystems.StorageClassName != "" {
		expectedSC[sc.Spec.ManagedResources.CephFilesystems.StorageClassName] = true
	}
	if sc.Spec.ManagedResources.CephNonResilientPools.StorageClassName != "" {
		expectedSC[sc.Spec.ManagedResources.CephNonResilientPools.StorageClassName] = true
	}
	if sc.Spec.NFS.StorageClassName != "" {
		expectedSC[sc.Spec.NFS.StorageClassName] = true
	}
	if sc.Spec.Encryption.StorageClassName != "" {
		expectedSC[sc.Spec.Encryption.StorageClassName] = true
	}

	for name := range expectedSC {
		if !currentSC[name] {
			return false, nil
		}
	}

	return true, nil
}
