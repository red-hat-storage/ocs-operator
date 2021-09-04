package storagecluster

import (
	"context"
	"fmt"
	"strings"

	quotav1 "github.com/openshift/api/quota/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type ocsStorageQuota struct{}

// ensureCreated ensures that all ClusterResourceQuota resources exists with their Spec in
// the desired state.
func (obj *ocsStorageQuota) ensureCreated(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) error {
	for _, opc := range sc.Spec.OverprovisionControl {
		hardLimit := hardLimitOf(sc, &opc)
		if hardLimit == nil {
			continue
		}
		storageQuota := &quotav1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: generateStorageQuotaName(opc.StorageClassName, opc.QuotaName)},
			Spec: quotav1.ClusterResourceQuotaSpec{
				Selector: opc.Selector,
				Quota: corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{resourceRequestName(opc.StorageClassName): *hardLimit},
				},
			},
		}

		currentQuota := &quotav1.ClusterResourceQuota{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: storageQuota.Name}, currentQuota)
		if err != nil {
			if !errors.IsNotFound(err) {
				r.Log.Error(err, fmt.Sprintf("get ClusterResourceQuota %s failed", storageQuota.Name))
				return err
			}
			r.Log.Info(fmt.Sprintf("creating ClusterResourceQuota %s with %+v", storageQuota.Name, storageQuota.Spec.Quota.Hard))
			err := r.Client.Create(context.TODO(), storageQuota)
			if err != nil {
				r.Log.Error(err, "create ClusterResourceQuota failed", storageQuota.Name)
				return err
			}
			continue
		}
		// Equality check of 'resource.Quantity' must be done with 'apiequality.Semantic.DeepEqual'
		// See: https://github.com/kubernetes/apimachinery/issues/75
		if !apiequality.Semantic.DeepEqual(storageQuota.Spec, currentQuota.Spec) {
			storageQuota.Spec.DeepCopyInto(&currentQuota.Spec)
			err = r.Client.Update(context.TODO(), currentQuota)
			if err != nil {
				r.Log.Error(err, "update ClusterResourceQuota failed", storageQuota.Name)
				return err
			}
		}
	}
	return nil
}

// ensureDeleted deletes all ClusterResourceQuota resources associated with StorageCluster
func (obj *ocsStorageQuota) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) error {
	for _, opc := range sc.Spec.OverprovisionControl {
		quotaName := generateStorageQuotaName(opc.StorageClassName, opc.QuotaName)
		currentQuota := &quotav1.ClusterResourceQuota{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: quotaName}, currentQuota)
		if err == nil {
			r.Log.Info("delete ClusterResourceQuota", quotaName)
			err = r.Client.Delete(context.TODO(), currentQuota)
			if err != nil {
				r.Log.Error(err, "delete ClusterResourceQuota failed", quotaName)
				return err
			}
		} else {
			r.Log.Error(err, "failed to get ClusterResourceQuota", quotaName)
		}
	}
	return nil
}

func resourceRequestName(storageClassName string) corev1.ResourceName {
	// storageClassSuffix is the suffix to the qualified portion of storage class resource name.
	// For example, if you want to quota storage by storage class, you would have a declaration
	// that follows <storage-class>.storageclass.storage.k8s.io/<resource>.
	// For example:
	//   ceph-rbd.storageclass.storage.k8s.io/requests.storage: 500Gi
	//
	// This pattern is required by k8s in MatchingResources and Usage calculations.
	// See:
	//  https://github.com/kubernetes/kubernetes/blob/master/pkg/quota/v1/evaluator/core/persistent_volume_claims.go#L53
	//  https://github.com/kubernetes/kubernetes/blob/master/pkg/quota/v1/evaluator/core/persistent_volume_claims.go#L120
	//  https://github.com/kubernetes/kubernetes/blob/master/pkg/quota/v1/evaluator/core/persistent_volume_claims.go#L146
	storageClassSuffix := ".storageclass.storage.k8s.io/"
	return corev1.ResourceName(storageClassName + storageClassSuffix + string(corev1.ResourceRequestsStorage))
}

func hardLimitOf(sc *ocsv1.StorageCluster, op *ocsv1.OverprovisionControlSpec) *resource.Quantity {
	if op.Capacity != nil {
		return op.Capacity
	}
	if op.Percentage > 0 {
		return resource.NewQuantity((calcUseableCapacity(sc)*int64(op.Percentage))/100, resource.BinarySI)
	}
	return nil
}

func calcUseableCapacity(sc *ocsv1.StorageCluster) int64 {
	var useableCapacity int64
	for _, ds := range sc.Spec.StorageDeviceSets {
		useableCapacity += useableCapacityOfDeviceSet(&ds)
	}
	return useableCapacity
}

func useableCapacityOfDeviceSet(ds *ocsv1.StorageDeviceSet) int64 {
	storageQuantity, ok := ds.DataPVCTemplate.Spec.Resources.Requests[corev1.ResourceStorage]
	if !ok {
		return 0
	}
	count, replica := countAndReplicaOf(ds)
	return int64(storageQuantity.AsApproximateFloat64()) * int64(count) * int64(replica)
}

// StorageClassByV1Resource returns storageclass name from resource name
func StorageClassByV1Resource(resourceName corev1.ResourceName) string {
	return strings.Split(resourceName.String(), ".")[0]
}
