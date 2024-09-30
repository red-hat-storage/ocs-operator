package clienttokens

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"

	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers"

	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var unitToGib = map[string]uint{
	"Gi": 1,
	"Ti": 1024,
	"Pi": 1024 * 1024,
}

func HandleMessage(w http.ResponseWriter, r *http.Request, tokenLifetimeInHours int, cl client.Client, namespace string) {
	switch r.Method {
	case "POST":
		handlePost(w, r, tokenLifetimeInHours, cl, namespace)
	default:
		handleUnsupportedMethod(w, r)
	}
}

func handlePost(w http.ResponseWriter, r *http.Request, tokenLifetimeInHours int, cl client.Client, namespace string) {
	var storageQuotaInGiB *uint
	// When ContentLength is 0 that means request body is empty and
	// storage quota is unlimited

	if r.ContentLength != 0 {
		var quota = struct {
			Value uint   `json:"value"`
			Unit  string `json:"unit"`
		}{}
		if err := json.NewDecoder(r.Body).Decode(&quota); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if quota.Value == 0 || quota.Value > math.MaxInt {
			http.Error(w, fmt.Sprintf("invalid value sent in request body, value should be greater than 0 and less than %v: %v", math.MaxInt, quota.Value), http.StatusBadRequest)
			return
		}
		unitAsGiB, ok := unitToGib[quota.Unit]
		if !ok {
			http.Error(w, fmt.Sprintf("invalid Unit type sent in request body, Valid types are [Gi,Ti,Pi]: %v", quota.Unit), http.StatusBadRequest)
			return
		}
		storageQuotaInGiB = ptr.To(unitAsGiB * quota.Value)
	}

	storageCluster, err := util.GetStorageClusterInNamespace(r.Context(), cl, namespace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	klog.Info("Loading onboarding validation private Key")
	privateKey, err := util.LoadOnboardingValidationPrivateKey(r.Context(), cl, namespace)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed loading onboarding validation private key: %v", err), http.StatusBadRequest)
		return
	}

	if onboardingToken, err := util.GenerateClientOnboardingToken(
		tokenLifetimeInHours,
		privateKey,
		storageQuotaInGiB,
		storageCluster.UID,
	); err != nil {
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
