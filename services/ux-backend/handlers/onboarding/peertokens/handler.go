package peertokens

import (
	"fmt"
	"net/http"

	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	onboardingPrivateKeyFilePath = "/etc/private-key/key"
)

func HandleMessage(w http.ResponseWriter, r *http.Request, tokenLifetimeInHours int, cl client.Client) {
	switch r.Method {
	case "POST":
		handlePost(w, r, tokenLifetimeInHours, cl)
	default:
		handleUnsupportedMethod(w, r)
	}
}

func handlePost(w http.ResponseWriter, _ *http.Request, tokenLifetimeInHours int, cl client.Client) {
	var err error

	ns := handlers.GetPodNamespace()

	privateKey, err := util.GetParsedPrivateKey(cl, ns)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get private key: %v", err), http.StatusBadRequest)
		return
	}

	if onboardingToken, err := util.GeneratePeerOnboardingToken(tokenLifetimeInHours, privateKey); err != nil {
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
