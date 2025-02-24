package webhook

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/go-logr/logr"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	storagev1 "k8s.io/api/storage/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type StorageClassAdmission struct {
	client.Client
	Decoder admission.Decoder
	Log     logr.Logger
}

var supportedProvisioners = []string{
	util.RbdDriverName,
	util.CephFSDriverName,
}

func (s *StorageClassAdmission) Handle(ctx context.Context, req admission.Request) admission.Response {
	s.Log.Info("Request received for admission review")

	storageClass := &storagev1.StorageClass{}
	if err := s.Decoder.Decode(req, storageClass); err != nil {
		s.Log.Error(err, "failed to decode admission review as storageclass")
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("only storageclasses admission reviews are supported: %v", err))
	}

	if !slices.Contains(supportedProvisioners, storageClass.Provisioner) {
		s.Log.Error(fmt.Errorf("unsupported provisioner %s", storageClass.Provisioner), "failed validation", "storageClass", storageClass.Name)
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("supported provisioners are %s", supportedProvisioners))
	}

	// TODO: mutate the storageclass with default parameters based on info from storagecluster

	return admission.Patched("TODO")
}
