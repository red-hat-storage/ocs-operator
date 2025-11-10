package storages

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"

	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"k8s.io/klog/v2"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	deploymentTypeInternal = "internal"
	deploymentTypeExternal = "external"
)

func HandleMessage(w http.ResponseWriter, r *http.Request, client ctrlclient.Client, namespace string) {
	switch r.Method {
	case "GET":
		handleGet(w, r, client, namespace)
	default:
		handleUnsupportedMethod(w, r)
	}
}

// request format: /info/storages
func handleGet(w http.ResponseWriter, r *http.Request, client ctrlclient.Client, namespace string) {
	result, err := getStorageInfo(r.Context(), client, namespace)
	if err != nil {
		klog.Errorf("failed to get storage info: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get storage info: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(result); err != nil {
		klog.Errorf("failed to encode response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

type storageInfoResponse struct {
	OperatorNamespace string                                   `json:"operatorNamespace"`
	ClusterNamespaces map[string]clusterNamespaceStorageInfo `json:"clusterNamespaces"`
}

type clusterNamespaceStorageInfo struct {
	StorageClusterName string `json:"storageClusterName"`
	DeploymentType     string `json:"deploymentType"`
	RgwSecureEndpoint  string `json:"rgwSecureEndpoint,omitempty"`
}

func getStorageInfo(ctx context.Context, client ctrlclient.Client, namespace string) (*storageInfoResponse, error) {
	klog.Info("Getting storage info")

	storageClusterList := &ocsv1.StorageClusterList{}
	if err := client.List(ctx, storageClusterList); err != nil {
		return nil, fmt.Errorf("failed to list StorageClusters: %w", err)
	}

	cephObjectStoreList := &cephv1.CephObjectStoreList{}
	if err := client.List(ctx, cephObjectStoreList); err != nil {
		return nil, fmt.Errorf("failed to list CephObjectStores: %w", err)
	}

	cosByNamespace := make(map[string]*cephv1.CephObjectStore)
	for i := range cephObjectStoreList.Items {
		cos := &cephObjectStoreList.Items[i]
		cosNamespace := cos.Namespace
		if _, exists := cosByNamespace[cosNamespace]; !exists {
			cosByNamespace[cosNamespace] = cos
		}
	}

	result := &storageInfoResponse{
		OperatorNamespace: namespace,
		ClusterNamespaces: make(map[string]clusterNamespaceStorageInfo),
	}

	for _, sc := range storageClusterList.Items {
		if sc.Status.Phase == util.PhaseIgnored {
			continue
		}

		scNamespace := sc.Namespace
		namespaceInfo := clusterNamespaceStorageInfo{
			StorageClusterName: sc.Name,
			DeploymentType:     getDeploymentType(&sc),
		}

		if cos, exists := cosByNamespace[scNamespace]; exists {
			rgwEndpoint := getRgwSecureEndpoint(cos)
			if rgwEndpoint != "" {
				namespaceInfo.RgwSecureEndpoint = rgwEndpoint
			}
		}

		result.ClusterNamespaces[scNamespace] = namespaceInfo
	}

	return result, nil
}

func getDeploymentType(sc *ocsv1.StorageCluster) string {
	if sc.Spec.ExternalStorage.Enable {
		return deploymentTypeExternal
	}

	return deploymentTypeInternal
}

func getRgwSecureEndpoint(cos *cephv1.CephObjectStore) string {
	if cos.Status == nil {
		return ""
	}

	if endpoint := cos.Status.Info["secureEndpoint"]; endpoint != "" {
		return endpoint
	}

	// fallback
	if len(cos.Status.Endpoints.Secure) > 0 {
		return cos.Status.Endpoints.Secure[0]
	}

	return ""
}

func handleUnsupportedMethod(w http.ResponseWriter, r *http.Request) {
	klog.Infof("Only GET method should be used for this endpoint %s", r.URL.Path)
	w.Header().Set("Content-Type", handlers.ContentTypeTextPlain)
	w.Header().Set("Allow", "GET")
	w.WriteHeader(http.StatusMethodNotAllowed)

	if _, err := w.Write([]byte(fmt.Sprintf("Unsupported method: %s", r.Method))); err != nil {
		klog.Errorf("failed to write data to response writer: %v", err)
	}
}
