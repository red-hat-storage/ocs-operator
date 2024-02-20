package rotatekeys

import (
	"context"
	"fmt"
	"net/http"

	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	onboardingValidationPublicKeySecretName = "onboarding-ticket-key"
)

func HandleMessage(w http.ResponseWriter, r *http.Request, cl client.Client) {
	switch r.Method {
	case "POST":
		handlePost(r.Context(), w, cl)
	default:
		handleUnsupportedMethod(w, r)
	}
}

func handlePost(ctx context.Context, w http.ResponseWriter, cl client.Client) {
	klog.Info("POST method on /rotate-keys endpoint is invoked")
	w.Header().Set("Content-Type", handlers.ContentTypeTextPlain)

	publicKeySecret := &corev1.Secret{}
	publicKeySecret.Name = onboardingValidationPublicKeySecretName
	publicKeySecret.Namespace = handlers.GetPodNamespace()
	err := cl.Delete(ctx, publicKeySecret)
	if err != nil {
		klog.Errorf("failed to delete public key secret: %v", err)
		w.WriteHeader(http.StatusInternalServerError)

		// TODO: should we differentiate b/n secret not found and remaining errors?
		if _, err = w.Write([]byte("Failed to rotate keys")); err != nil {
			klog.Errorf("failed to write data to response writer, %v", err)
		}
		return
	}

	klog.Info("onboarding validation keys are rotated successfully")
	w.WriteHeader(http.StatusOK)
	if _, err = w.Write([]byte("Successfully rotated keys")); err != nil {
		klog.Errorf("failed to write data to response writer, %v", err)
	}
}

func handleUnsupportedMethod(w http.ResponseWriter, r *http.Request) {
	klog.Info("Only POST method should be used to send data to this endpoint /rotate-keys")
	w.WriteHeader(http.StatusMethodNotAllowed)
	w.Header().Set("Content-Type", handlers.ContentTypeTextPlain)
	w.Header().Set("Allow", "POST")

	if _, err := w.Write([]byte(fmt.Sprintf("Unsupported method : %s", r.Method))); err != nil {
		klog.Errorf("failed to write data to response writer: %v", err)
	}
}
