package onboardingtokens

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/services"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers"
	"k8s.io/klog/v2"
)

const (
	onboardingPrivateKeyFilePath = "/etc/private-key/key"
)

func HandleMessage(w http.ResponseWriter, r *http.Request, tokenLifetimeInHours int) {
	switch r.Method {
	case "POST":
		handlePost(w, r, tokenLifetimeInHours)
	default:
		handleUnsupportedMethod(w, r)
	}
}

func handlePost(w http.ResponseWriter, r *http.Request, tokenLifetimeInHours int) {
	var role services.Role
	if r.ContentLength != 0 {
		var body = struct {
			Role string `json:"role"`
		}{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if strings.ToLower(body.Role) == string(services.MirroringPeerRole) {
			role = services.MirroringPeerRole
		} else if strings.ToLower(body.Role) == string(services.ClientRole) || body.Role == "" {
			// to ensure backward compatibility, if the role is empty, we assume it to be client
			role = services.ClientRole
		} else {
			http.Error(w, fmt.Sprintf("invalid Role sent in request body, Valid types are [mirroring-peer,client]: %v", body.Role), http.StatusBadRequest)
			return
		}
	}
	if onboardingToken, err := util.GenerateOnboardingToken(tokenLifetimeInHours, onboardingPrivateKeyFilePath, role); err != nil {
		klog.Errorf("failed to get onboardig token: %v", err)
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
	klog.Info("Only POST method should be used to send data to this endpoint /onboarding-tokens")
	w.WriteHeader(http.StatusMethodNotAllowed)
	w.Header().Set("Content-Type", handlers.ContentTypeTextPlain)
	w.Header().Set("Allow", "POST")

	if _, err := w.Write([]byte(fmt.Sprintf("Unsupported method : %s", r.Method))); err != nil {
		klog.Errorf("failed write data to response writer: %v", err)
	}
}
