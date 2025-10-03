package bucketorigin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers"
	nbv1alpha1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func HandleMessage(w http.ResponseWriter, r *http.Request, client client.Client) {
	switch r.Method {
	case "GET":
		handleGet(w, r, client)
	default:
		handleUnsupportedMethod(w, r)
	}
}

// request format: /auth/bucket-origin?name=<bucket-name>
func handleGet(w http.ResponseWriter, r *http.Request, client client.Client) {
	bucketName := r.URL.Query().Get("name")
	if bucketName == "" {
		klog.Errorf("name parameter is required")
		http.Error(w, "name parameter is required", http.StatusBadRequest)
		return
	}

	createdViaOBC, err := checkBucketCreatedViaOBC(client, bucketName, r.Context())
	if err != nil {
		klog.Errorf("failed to check bucket origin: %v", err)
		http.Error(w, fmt.Sprintf("Failed to check bucket origin: %v", err), http.StatusInternalServerError)
		return
	}

	result := map[string]bool{
		"createdViaOBC": createdViaOBC,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(result); err != nil {
		klog.Errorf("failed to encode response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func checkBucketCreatedViaOBC(client client.Client, bucketName string, ctx context.Context) (bool, error) {
	klog.Infof("Checking if bucket '%s' was created via OBC", bucketName)
	
	objectBucketList := &nbv1alpha1.ObjectBucketList{}
	if err := client.List(ctx, objectBucketList); err != nil {
		return false, fmt.Errorf("failed to list ObjectBuckets: %w", err)
	}
	
	for _, ob := range objectBucketList.Items {
		if ob.Spec.Endpoint != nil && ob.Spec.Endpoint.BucketName == bucketName {
			klog.Infof("Found ObjectBucket '%s' with bucket name '%s'", ob.Name, bucketName)
			return true, nil
		}
	}
	
	klog.Infof("No ObjectBucket found with bucket name '%s'", bucketName)
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
