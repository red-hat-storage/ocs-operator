package util

import (
	"context"
	"fmt"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"strings"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	// WatchNamespaceEnvVar is the constant for env variable WATCH_NAMESPACE
	// which is the namespace where the watch activity happens.
	// this value is empty if the operator is running with clusterScope.
	WatchNamespaceEnvVar = "WATCH_NAMESPACE"

	// PodNamespaceEnvVar is the env variable for the pod namespace
	PodNamespaceEnvVar = "POD_NAMESPACE"

	// SingleNodeEnvVar is set if StorageCluster needs to be deployed on a single node
	SingleNodeEnvVar = "SINGLE_NODE"

	// This configmap is purely for the OCS operator to use.
	OcsOperatorConfigName = "ocs-operator-config"

	// This configmap is watched by rook-ceph-operator & is reserved only for manual overrides.
	RookCephOperatorConfigName = "rook-ceph-operator-config"

	// These are the keys in the ocs-operator-config configmap
	ClusterNameKey              = "CSI_CLUSTER_NAME"
	RookCurrentNamespaceOnlyKey = "ROOK_CURRENT_NAMESPACE_ONLY"
	EnableTopologyKey           = "CSI_ENABLE_TOPOLOGY"
	TopologyDomainLabelsKey     = "CSI_TOPOLOGY_DOMAIN_LABELS"
	EnableNFSKey                = "ROOK_CSI_ENABLE_NFS"
	DisableCSIDriverKey         = "ROOK_CSI_DISABLE_DRIVER"

	// This is the name for the OwnerUID FieldIndex
	OwnerUIDIndexName = "ownerUID"

	OdfInfoNamespacedNameClaimName      = "odfinfo.odf.openshift.io"
	ExitCodeThatShouldRestartTheProcess = 42

	BlockPoolMirroringTargetIDAnnotation = "ocs.openshift.io/mirroring-target-id"
	RequestMaintenanceModeAnnotation     = "ocs.openshift.io/request-maintenance-mode"
	CephRBDMirrorName                    = "cephrbdmirror"
)

var podNamespace = os.Getenv(PodNamespaceEnvVar)

// GetPodNamespace returns the namespace where the pod is deployed
func GetPodNamespace() string {
	if podNamespace == "" {
		panic(fmt.Errorf("%s must be set", PodNamespaceEnvVar))
	}
	return podNamespace
}

// GetWatchNamespace returns the namespace the operator should be watching for changes
func GetWatchNamespace() (string, error) {
	ns, found := os.LookupEnv(WatchNamespaceEnvVar)
	if !found {
		return "", fmt.Errorf("%s must be set", WatchNamespaceEnvVar)
	}
	return ns, nil
}

// OperatorNamespaceEnvVar is the constant for env variable OPERATOR_NAMESPACE
// which is the namespace where operator pod is deployed.
const OperatorNamespaceEnvVar = "OPERATOR_NAMESPACE"

// GetOperatorNamespace returns the namespace where the operator is deployed.
func GetOperatorNamespace() (string, error) {
	ns, found := os.LookupEnv(OperatorNamespaceEnvVar)
	if !found {
		return "", fmt.Errorf("%s must be set", OperatorNamespaceEnvVar)
	}
	return ns, nil
}

// IsSingleNodeDeployment returns true if StorageCluster needs to be deployed on a single node.
func IsSingleNodeDeployment() bool {
	isSingleNode := os.Getenv(SingleNodeEnvVar)
	return strings.ToLower(strings.TrimSpace(isSingleNode)) == "true"
}

// getClusterID returns the cluster ID of the OCP-Cluster
func GetClusterID(ctx context.Context, kubeClient client.Client, logger *logr.Logger) string {
	clusterVersion := &configv1.ClusterVersion{}
	err := kubeClient.Get(ctx, types.NamespacedName{Name: "version"}, clusterVersion)
	if err != nil {
		logger.Error(err, "Failed to get the clusterVersion version of the OCP cluster")
		return ""
	}
	return fmt.Sprint(clusterVersion.Spec.ClusterID)
}

// RestartPod restarts the pod with the given name in the given namespace by deleting it and letting another one be created
func RestartPod(ctx context.Context, kubeClient client.Client, logger *logr.Logger, name string, namespace string) {
	logger.Info("restarting pod", "name", name, "namespace", namespace)
	podList := &corev1.PodList{}
	err := kubeClient.List(ctx, podList, client.InNamespace(namespace))
	if err != nil {
		logger.Error(err, "failed to list pods", "namespace", namespace)
		return
	}
	for _, pod := range podList.Items {
		if strings.Contains(pod.Name, name) {
			err = kubeClient.Delete(ctx, &pod)
			if err != nil {
				logger.Error(err, "failed to delete pod", "name", pod.Name, "namespace", namespace)
			}
		}
	}
}

// GetPodsWithLabels gives all the pods that are in a namespace after filtering them based on the given label selector
func GetPodsWithLabels(ctx context.Context, kubeClient client.Client, namespace string, labelSelector map[string]string) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	if err := kubeClient.List(ctx, podList, client.InNamespace(namespace), &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labelSelector),
	}); err != nil {
		return nil, err
	}
	return podList, nil
}

// GetStorageClassWithName returns the storage class object by name
func GetStorageClassWithName(ctx context.Context, kubeClient client.Client, name string) *storagev1.StorageClass {
	sc := &storagev1.StorageClass{}
	err := kubeClient.Get(ctx, types.NamespacedName{Name: name}, sc)
	if err != nil {
		return nil
	}
	return sc
}

// getCountOfRunningPods gives the count of pods in running state in a given pod list
func GetCountOfRunningPods(podList *corev1.PodList) int {
	count := 0
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodRunning {
			count++
		}
	}
	return count
}

func OwnersIndexFieldFunc(obj client.Object) []string {
	refs := obj.GetOwnerReferences()
	owners := []string{}
	for i := range refs {
		owners = append(owners, string(refs[i].UID))
	}
	return owners
}

func GenerateNameForNonResilientCephBlockPoolSC(initData *ocsv1.StorageCluster) string {
	if initData.Spec.ManagedResources.CephNonResilientPools.StorageClassName != "" {
		return initData.Spec.ManagedResources.CephNonResilientPools.StorageClassName
	}
	return fmt.Sprintf("%s-ceph-non-resilient-rbd", initData.Name)
}

func GetStorageClusterInNamespace(ctx context.Context, cl client.Client, namespace string) (*ocsv1.StorageCluster, error) {
	storageClusterList := &ocsv1.StorageClusterList{}
	err := cl.List(ctx, storageClusterList, client.InNamespace(namespace), client.Limit(1))
	if err != nil {
		return nil, fmt.Errorf("unable to list storageCluster(s) in namespace %s: %v", namespace, err)
	}

	if len(storageClusterList.Items) == 0 {
		return nil, fmt.Errorf("no storageCluster found in namespace %s", namespace)
	}
	if storageClusterList.Items[0].Status.Phase == PhaseIgnored {
		return nil, fmt.Errorf("storageCluster with Phase 'Ignored' found. Please delete the storageCluster to proceed")
	}

	return &storageClusterList.Items[0], nil
}

func NewK8sClient(scheme *runtime.Scheme) (client.Client, error) {
	klog.Info("Setting up k8s client")

	config, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}

	return k8sClient, nil
}

func FindOwnerRefByKind(obj client.Object, kind string) *v1.OwnerReference {
	owners := obj.GetOwnerReferences()
	for i := range owners {
		if owners[i].Kind == kind {
			return &owners[i]
		}
	}
	return nil
}
