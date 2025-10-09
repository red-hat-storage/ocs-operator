package featureflags

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers"
	nbv1alpha1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/klog/v2"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	FlagNoobaa string = "noobaa"
	FlagRGW    string = "rgw"
)

func HandleMessage(w http.ResponseWriter, r *http.Request, client ctrlclient.Client, namespace string) {
	switch r.Method {
	case "GET":
		handleGet(w, r, client, namespace)
	default:
		handleUnsupportedMethod(w, r)
	}
}

// request format (comma-separated flags): /auth/featureflags?flags=noobaa,rgw
func handleGet(w http.ResponseWriter, r *http.Request, client ctrlclient.Client, namespace string) {
	flagsParam := r.URL.Query().Get("flags")
	if flagsParam == "" {
		klog.Errorf("flags parameter is required")
		http.Error(w, "flags parameter is required", http.StatusBadRequest)
		return
	}

	flagNames := strings.Split(flagsParam, ",")
	for i, flag := range flagNames {
		flagNames[i] = strings.TrimSpace(flag)
	}

	type flagResult struct {
		Value bool   `json:"value"`
		Error string `json:"error,omitempty"`
	}

	result := make(map[string]flagResult)
	for _, flagName := range flagNames {
		switch flagName {
		case FlagNoobaa:
			value, err := checkNoobaaFlag(client, namespace, r.Context())
			if err != nil {
				klog.Errorf("failed to check noobaa flag: %v", err)
				result[flagName] = flagResult{Value: false, Error: err.Error()}
			} else {
				result[flagName] = flagResult{Value: value}
			}
		case FlagRGW:
			value, err := checkRGWFlag(client, r.Context())
			if err != nil {
				klog.Errorf("failed to check rgw flag: %v", err)
				result[flagName] = flagResult{Value: false, Error: err.Error()}
			} else {
				result[flagName] = flagResult{Value: value}
			}
		default:
			result[flagName] = flagResult{Value: false, Error: "unsupported flag"}
		}
	}

	w.Header().Set("Content-Type", "application/json")

	allFlagsFailed := true
	for _, res := range result {
		if res.Error == "" {
			allFlagsFailed = false
			break
		}
	}

	// if all flags failed, returning "500" as response
	// if some flags succeeded, returning "200" with partial results and error details
	if allFlagsFailed && len(result) > 0 {
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	if err := json.NewEncoder(w).Encode(result); err != nil {
		klog.Errorf("failed to encode response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func checkNoobaaFlag(client ctrlclient.Client, namespace string, ctx context.Context) (bool, error) {
	noobaaExists, err := checkNoobaaCRExists(client, namespace, ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check NooBaa CR: %w", err)
	}
	if !noobaaExists {
		return false, nil
	}

	storageClassExists, err := checkNoobaaStorageClassExists(client, ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check NooBaa StorageClass: %w", err)
	}

	return storageClassExists, nil
}

func checkNoobaaCRExists(client ctrlclient.Client, namespace string, ctx context.Context) (bool, error) {
	klog.Info("Checking NooBaa CR existence in namespace:", namespace)

	noobaaList := &nbv1alpha1.NooBaaList{}
	if err := client.List(ctx, noobaaList, ctrlclient.InNamespace(namespace)); err != nil {
		return false, fmt.Errorf("failed to list NooBaa CRs: %w", err)
	}

	if len(noobaaList.Items) > 0 {
		klog.Info("Found NooBaa CRs in namespace:", namespace, "count:", len(noobaaList.Items))
		return true, nil
	}

	klog.Info("No NooBaa CRs found in namespace:", namespace)
	return false, nil
}

func checkNoobaaStorageClassExists(client ctrlclient.Client, ctx context.Context) (bool, error) {
	klog.Info("Checking for NooBaa StorageClass existence")
	
	storageClassList := &storagev1.StorageClassList{}
	if err := client.List(ctx, storageClassList); err != nil {
		return false, fmt.Errorf("failed to list StorageClasses: %w", err)
	}
	
	for _, sc := range storageClassList.Items {
		if strings.HasSuffix(sc.Provisioner, "noobaa.io/obc") {
			klog.Info("Found NooBaa StorageClass:", sc.Name, "with provisioner:", sc.Provisioner)
			return true, nil
		}
	}
	
	klog.Info("No NooBaa StorageClasses found")
	return false, nil
}

func checkRGWFlag(client ctrlclient.Client, ctx context.Context) (bool, error) {
	cephObjectStoreExists, err := checkCephObjectStoreExists(client, ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check CephObjectStore: %w", err)
	}
	if !cephObjectStoreExists {
		return false, nil
	}

	storageClassExists, err := checkRGWStorageClassExists(client, ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check RGW StorageClass: %w", err)
	}

	return storageClassExists, nil
}

func checkCephObjectStoreExists(client ctrlclient.Client, ctx context.Context) (bool, error) {
	klog.Info("Checking CephObjectStore existence")

	cephObjectStoreList := &cephv1.CephObjectStoreList{}
	if err := client.List(ctx, cephObjectStoreList); err != nil {
		return false, fmt.Errorf("failed to list CephObjectStores: %w", err)
	}

	if len(cephObjectStoreList.Items) > 0 {
		klog.Info("Found CephObjectStores, count:", len(cephObjectStoreList.Items))
		return true, nil
	}

	klog.Info("No CephObjectStores found")
	return false, nil
}

func checkRGWStorageClassExists(client ctrlclient.Client, ctx context.Context) (bool, error) {
	klog.Info("Checking for RGW StorageClass existence")
	
	storageClassList := &storagev1.StorageClassList{}
	if err := client.List(ctx, storageClassList); err != nil {
		return false, fmt.Errorf("failed to list StorageClasses: %w", err)
	}
	
	for _, sc := range storageClassList.Items {
		if strings.HasSuffix(sc.Provisioner, "ceph.rook.io/bucket") {
			klog.Info("Found RGW StorageClass:", sc.Name, "with provisioner:", sc.Provisioner)
			return true, nil
		}
	}
	
	klog.Info("No RGW StorageClasses found")
	return false, nil
}

func handleUnsupportedMethod(w http.ResponseWriter, r *http.Request) {
	klog.Infof("Only GET method should be used for this endpoint %s", r.URL.Path)
	w.WriteHeader(http.StatusMethodNotAllowed)
	w.Header().Set("Content-Type", handlers.ContentTypeTextPlain)
	w.Header().Set("Allow", "GET")

	if _, err := w.Write([]byte(fmt.Sprintf("Unsupported method: %s", r.Method))); err != nil {
		klog.Errorf("failed to write data to response writer: %v", err)
	}
}
