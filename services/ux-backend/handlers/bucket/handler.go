package bucket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers"

	nbv1a1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	uxutil "github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/util"
	"k8s.io/klog/v2"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	creationMethodOBC = "obc"
	creationMethodS3  = "s3"
)

func HandleMessage(w http.ResponseWriter, r *http.Request, client ctrlclient.Client) {
	switch r.Method {
	case "GET":
		handleGet(w, r, client)
	default:
		handleUnsupportedMethod(w, r)
	}
}

// request format: /info/bucket/<bucket-name>
func handleGet(w http.ResponseWriter, r *http.Request, client ctrlclient.Client) {
	bucketName := r.PathValue("name")
	if bucketName == "" {
		klog.Errorf("bucket name is required in path")
		http.Error(w, "bucket name is required in path", http.StatusBadRequest)
		return
	}

	creationMethod, err := getBucketCreationMethod(r.Context(), client, bucketName)
	if err != nil {
		klog.Errorf("failed to get bucket creation method: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get bucket creation method: %v", err), http.StatusInternalServerError)
		return
	}

	result := map[string]string{
		"createdVia": creationMethod,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(result); err != nil {
		klog.Errorf("failed to encode response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func getBucketCreationMethod(ctx context.Context, client ctrlclient.Client, bucketName string) (string, error) {
	klog.Infof("Getting creation method for bucket '%s'", bucketName)
	
	objectBucketList := &nbv1a1.ObjectBucketList{}
	if err := client.List(ctx, objectBucketList, ctrlclient.MatchingFields{uxutil.IndexBucketName: bucketName}, ctrlclient.Limit(1)); err != nil {
		return "", fmt.Errorf("failed to list ObjectBuckets by bucketName index: %w", err)
	}
	
	if len(objectBucketList.Items) > 0 {
		klog.Infof("Found ObjectBucket '%s' with bucket name '%s'", objectBucketList.Items[0].Name, bucketName)
		return creationMethodOBC, nil
	}
	
	// There are two ways to create a bucket:
	// 1. Either via OBC (ObjectBucketClaim) CR
	// 2. Or via S3 endpoint directly
	// This endpoint is only called on existing buckets, so we assume S3 creation if no OBC is found.
	klog.Infof("No ObjectBucket found with bucket name '%s', assuming S3 creation", bucketName)
	return creationMethodS3, nil
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
