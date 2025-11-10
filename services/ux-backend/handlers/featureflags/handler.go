package featureflags

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers"

	nbv1a1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/klog/v2"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	flagNoobaa string = "noobaa"
	flagRGW    string = "rgw"
)

func HandleMessage(w http.ResponseWriter, r *http.Request, client ctrlclient.Client, namespace string) {
	switch r.Method {
	case "GET":
		handleGet(w, r, client, namespace)
	default:
		handleUnsupportedMethod(w, r)
	}
}

// request format (comma-separated flags): /info/featureflags?flags=noobaa,rgw
func handleGet(w http.ResponseWriter, r *http.Request, client ctrlclient.Client, namespace string) {
	flagsQuery := r.URL.Query().Get("flags")
	if flagsQuery == "" {
		klog.Errorf("flags parameter is required")
		http.Error(w, "flags parameter is required", http.StatusBadRequest)
		return
	}

	flagNames := strings.Split(flagsQuery, ",")
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
		case flagNoobaa:
			value, err := checkNoobaaFlag(r.Context(), client, namespace)
			if err != nil {
				klog.Errorf("failed to check noobaa flag: %v", err)
				result[flagName] = flagResult{Value: false, Error: err.Error()}
			} else {
				result[flagName] = flagResult{Value: value}
			}
		case flagRGW:
			value, err := checkRGWFlag(r.Context(), client, namespace)
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

func checkNoobaaFlag(ctx context.Context, client ctrlclient.Client, namespace string) (bool, error) {
	klog.Info("Checking NooBaa CR existence in namespace:", namespace)
	noobaa := &nbv1a1.NooBaa{}
	if err := client.Get(ctx, ctrlclient.ObjectKey{Name: util.NoobaaResourceName, Namespace: namespace}, noobaa); err != nil {
		if ctrlclient.IgnoreNotFound(err) == nil {
			klog.Info("No NooBaa CR found in namespace:", namespace)
			return false, nil
		}
		return false, fmt.Errorf("failed to get NooBaa CR: %w", err)
	}
	klog.Info("Found NooBaa CR in namespace:", namespace)

	klog.Info("Checking for NooBaa StorageClass existence")
	storageClassList := &storagev1.StorageClassList{}
	if err := client.List(ctx, storageClassList); err != nil {
		return false, fmt.Errorf("failed to list StorageClasses: %w", err)
	}

	for _, sc := range storageClassList.Items {
		if strings.HasSuffix(sc.Provisioner, util.NoobaaDriverNameSuffix) {
			klog.Info("Found NooBaa StorageClass:", sc.Name, "with provisioner:", sc.Provisioner)
			return true, nil
		}
	}

	klog.Info("No NooBaa StorageClasses found")
	return false, nil
}

func checkRGWFlag(ctx context.Context, client ctrlclient.Client, namespace string) (bool, error) {
	klog.Info("Checking CephObjectStore existence")
	cephObjectStoreList := &cephv1.CephObjectStoreList{}
	if err := client.List(ctx, cephObjectStoreList, ctrlclient.Limit(1)); err != nil {
		return false, fmt.Errorf("failed to list CephObjectStores: %w", err)
	}

	if len(cephObjectStoreList.Items) == 0 {
		klog.Info("No CephObjectStores found")
		return false, nil
	}
	klog.Info("Found CephObjectStore")

	klog.Info("Checking for RGW StorageClass existence")
	storageClassList := &storagev1.StorageClassList{}
	if err := client.List(ctx, storageClassList); err != nil {
		return false, fmt.Errorf("failed to list StorageClasses: %w", err)
	}

	for _, sc := range storageClassList.Items {
		if strings.HasSuffix(sc.Provisioner, util.ObcDriverNameSuffix) {
			klog.Info("Found RGW StorageClass:", sc.Name, "with provisioner:", sc.Provisioner)
			return true, nil
		}
	}

	klog.Info("No RGW StorageClasses found")
	return false, nil
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
