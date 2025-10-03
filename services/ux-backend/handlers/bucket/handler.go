package bucket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers"
	nbv1alpha1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	CreationMethodOBC = "obc"
	CreationMethodS3  = "s3"
)

func HandleMessage(w http.ResponseWriter, r *http.Request, client client.Client) {
	switch r.Method {
	case "GET":
		handleGet(w, r, client)
	default:
		handleUnsupportedMethod(w, r)
	}
}

// request format: /auth/bucket/<bucket-name>
func handleGet(w http.ResponseWriter, r *http.Request, client client.Client) {
	path := strings.TrimPrefix(r.URL.Path, "/auth/bucket/")
	if path == "" || path == r.URL.Path {
		klog.Errorf("bucket name is required in path")
		http.Error(w, "bucket name is required in path", http.StatusBadRequest)
		return
	}

	bucketName := path

	createdVia, err := checkBucketCreatedViaOBC(client, bucketName, r.Context())
	if err != nil {
		klog.Errorf("failed to check bucket origin: %v", err)
		http.Error(w, fmt.Sprintf("Failed to check bucket origin: %v", err), http.StatusInternalServerError)
		return
	}

	var creationMethod string
	if createdVia {
		creationMethod = CreationMethodOBC
	} else {
		creationMethod = CreationMethodS3
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
	w.Header().Set("Content-Type", handlers.ContentTypeTextPlain)
	w.Header().Set("Allow", "GET")
	w.WriteHeader(http.StatusMethodNotAllowed)

	if _, err := w.Write([]byte(fmt.Sprintf("Unsupported method: %s", r.Method))); err != nil {
		klog.Errorf("failed to write data to response writer: %v", err)
	}
}
