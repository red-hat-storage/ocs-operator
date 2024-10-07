package peertokens

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	onboardingPrivateKeyFilePath = "/etc/private-key/key"
)

func HandleMessage(ctx context.Context, w http.ResponseWriter, r *http.Request, tokenLifetimeInHours int, cl client.Client) {
	switch r.Method {
	case "POST":
		handlePost(ctx, w, r, tokenLifetimeInHours, cl)
	default:
		handleUnsupportedMethod(w, r)
	}
}

func handlePost(ctx context.Context, w http.ResponseWriter, r *http.Request, tokenLifetimeInHours int, cl client.Client) {
	if r.ContentLength == 0 {
		http.Error(w, "Request body is empty, expected StorageClusterNamespacedName", http.StatusBadRequest)
		return
	}

	var body = struct {
		StorageClusterNamespacedName string `json:"storageClusterNamespacedName"`
	}{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	parts := strings.Split(body.StorageClusterNamespacedName, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.Error(w, "storageClusterNamespacedName is not in expected format", http.StatusBadRequest)
	}

	storageCluster := &ocsv1.StorageCluster{}
	storageCluster.Namespace = parts[0]
	storageCluster.Name = parts[1]

	err := cl.Get(ctx, client.ObjectKeyFromObject(storageCluster), storageCluster)
	if err != nil {
		http.Error(w,
			fmt.Sprintf("failed to find StorageCluster with NamespacedName %s", body.StorageClusterNamespacedName),
			http.StatusBadRequest)
		return
	}

	if onboardingToken, err := util.GeneratePeerOnboardingToken(tokenLifetimeInHours, onboardingPrivateKeyFilePath, string(storageCluster.UID)); err != nil {
		klog.Errorf("failed to get onboarding token: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", handlers.ContentTypeTextPlain)

		if _, err := w.Write([]byte("Failed to generate token")); err != nil {
			klog.Errorf("failed write data to response writer, %v", err)
		}
	} else {
		klog.Info("onboarding token generated successfully")
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", handlers.ContentTypeTextPlain)

		if _, err = w.Write([]byte(onboardingToken)); err != nil {
			klog.Errorf("failed write data to response writer: %v", err)
		}
	}
}

func handleUnsupportedMethod(w http.ResponseWriter, r *http.Request) {
	klog.Infof("Only POST method should be used to send data to this endpoint %s", r.URL.Path)
	w.WriteHeader(http.StatusMethodNotAllowed)
	w.Header().Set("Content-Type", handlers.ContentTypeTextPlain)
	w.Header().Set("Allow", "POST")

	if _, err := w.Write([]byte(fmt.Sprintf("Unsupported method : %s", r.Method))); err != nil {
		klog.Errorf("failed write data to response writer: %v", err)
	}
}
