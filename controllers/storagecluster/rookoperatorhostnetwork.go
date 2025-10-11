package storagecluster

import (
	"fmt"
	"slices"

	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type rookCephCsvHostNetwork struct{}

// if the StorageCluster is configured to run on non-default host network, rook-ceph-operator needs to run on host network as well
// this is validated by checking if the AddressRanges.Public field is set in the Network spec
// since pod network cannot always communicate with non-default host network rook-ceph-operator needs to run on host network
func (obj *rookCephCsvHostNetwork) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {

	csvList := &opv1a1.ClusterServiceVersionList{}
	labelSelector := labels.Set{
		fmt.Sprintf("operators.coreos.com/rook-ceph-operator.%s", instance.Namespace): "",
	}.AsSelector()
	listOpts := &client.ListOptions{
		Namespace:     instance.Namespace,
		LabelSelector: labelSelector,
	}

	// List the ClusterServiceVersion for rook-ceph-operator
	err := r.Client.List(r.ctx, csvList, listOpts)
	if err != nil {
		return reconcile.Result{}, err
	}

	var rookCSV *opv1a1.ClusterServiceVersion
	for i := range csvList.Items {
		csv := &csvList.Items[i]
		if csv.Status.Phase == opv1a1.CSVPhaseSucceeded {
			rookCSV = csv
			break
		}
	}

	if rookCSV == nil {
		return reconcile.Result{}, fmt.Errorf("no rook-ceph-operator CSV found in Succeeded phase")
	}

	deployments := rookCSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs
	index := slices.IndexFunc(deployments, func(d opv1a1.StrategyDeploymentSpec) bool {
		return d.Name == "rook-ceph-operator"
	})

	if index == -1 {
		return reconcile.Result{}, fmt.Errorf("rook-ceph-operator deployment not found in CSV")
	}

	deployment := &rookCSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs[index]
	if shouldUseHostNetworking(instance) {
		deployment.Spec.Template.Spec.HostNetwork = true
		deployment.Spec.Template.Spec.DNSPolicy = corev1.DNSClusterFirstWithHostNet
	} else {
		deployment.Spec.Template.Spec.HostNetwork = false
		deployment.Spec.Template.Spec.DNSPolicy = corev1.DNSClusterFirst
	}

	if err := r.Client.Update(r.ctx, rookCSV); err != nil {
		r.Log.Error(err, "Failed to update rook-ceph-operator CSV.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (obj *rookCephCsvHostNetwork) ensureDeleted(_ *StorageClusterReconciler, _ *ocsv1.StorageCluster) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

// shouldUseHostNetworking checks if the StorageCluster is configured to run on non-default host network
// this is validated by checking if the AddressRanges.Public field is set in the Network spec
// since pod network cannot always communicate with non-default host network pods needs to run on host net
func shouldUseHostNetworking(instance *ocsv1.StorageCluster) bool {
	if !isMultus(instance.Spec.Network) &&
		instance.Spec.Network != nil &&
		instance.Spec.Network.AddressRanges != nil &&
		instance.Spec.Network.AddressRanges.Public != nil {
		return true
	}
	return false
}
