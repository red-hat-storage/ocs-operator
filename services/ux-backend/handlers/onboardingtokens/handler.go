package onboardingtokens

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/services"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers"

	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

const (
	onboardingPrivateKeyFilePath = "/etc/private-key/key"
)

var unitToGib = map[string]uint{
	"Gi": 1,
	"Ti": 1024,
	"Pi": 1024 * 1024,
}

func HandleMessage(w http.ResponseWriter, r *http.Request, tokenLifetimeInHours int) {
	switch r.Method {
	case "POST":
		handlePost(w, r, tokenLifetimeInHours)
	default:
		handleUnsupportedMethod(w, r)
	}
}

func handlePost(w http.ResponseWriter, r *http.Request, tokenLifetimeInHours int) {
	var storageQuotaInGiB *uint
	var role services.OnboardingSubjectRole
	// When ContentLength is 0 that means request body is empty and
	// storage quota is unlimited
	var err error
	if r.ContentLength != 0 {
		var body = struct {
			SubjectRole string `json:"subjectRole"`
			Value       uint   `json:"value"`
			Unit        string `json:"unit"`
		}{}
		if err = json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if strings.ToLower(body.SubjectRole) == string(services.PeerRole) {
			role = services.PeerRole
		} else if strings.ToLower(body.SubjectRole) == string(services.ClientRole) || body.SubjectRole == "" {
			// to ensure backward compatibility, if the role is empty, we assume it to be client
			role = services.ClientRole
			if body.Value == 0 || body.Value > math.MaxInt {
				http.Error(w, fmt.Sprintf("invalid value sent in request body, value should be greater than 0 and less than %v: %v", math.MaxInt, body.Value), http.StatusBadRequest)
				return
			}
			unitAsGiB, ok := unitToGib[body.Unit]
			if !ok {
				http.Error(w, fmt.Sprintf("invalid Unit type sent in request body, Valid types are [Gi,Ti,Pi]: %v", body.Unit), http.StatusBadRequest)
				return
			}
			storageQuotaInGiB = ptr.To(unitAsGiB * body.Value)
		} else {
			http.Error(w, fmt.Sprintf("invalid OnboardingSubjectRole sent in request body, Valid roles are [mirroring-peer,client]: %v", body.SubjectRole), http.StatusBadRequest)
			return
		}
	}
	if onboardingToken, err := util.GenerateOnboardingToken(tokenLifetimeInHours, onboardingPrivateKeyFilePath, storageQuotaInGiB, role); err != nil {
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
	klog.Info("Only POST method should be used to send data to this endpoint /onboarding-tokens")
	w.WriteHeader(http.StatusMethodNotAllowed)
	w.Header().Set("Content-Type", handlers.ContentTypeTextPlain)
	w.Header().Set("Allow", "POST")

	if _, err := w.Write([]byte(fmt.Sprintf("Unsupported method : %s", r.Method))); err != nil {
		klog.Errorf("failed write data to response writer: %v", err)
	}
}
