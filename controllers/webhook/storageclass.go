package webhook

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"

	"github.com/go-logr/logr"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	jsonpatch "gomodules.xyz/jsonpatch/v2"
	storagev1 "k8s.io/api/storage/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type StorageClassAdmission struct {
	client.Client
	Namespace string
	Decoder   admission.Decoder
	Log       logr.Logger
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

	storageConsumerList := &v1alpha1.StorageConsumerList{}
	if err := s.List(ctx, storageConsumerList, client.InNamespace(s.Namespace)); err != nil {
		s.Log.Error(err, "failed to list storageconsumers", "namespace", s.Namespace)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to list storageconsumers in %s namespace", s.Namespace))
	}

	if len(storageConsumerList.Items) == 0 {
		s.Log.Error(fmt.Errorf("no storageconsumers found in %s namespace", s.Namespace), "failed validation")
		return admission.Denied("creation of storageclass should happen after storageconsumer exists")
	}

	clusterID := ""
	for idx := range storageConsumerList.Items {
		consumer := &storageConsumerList.Items[idx]
		if consumer.Annotations[defaults.StorageConsumerTypeAnnotation] == defaults.StorageConsumerTypeLocal {
			clusterID = string(consumer.UID)
			break
		}
	}

	if clusterID == "" {
		s.Log.Error(fmt.Errorf("no storageconsumer is marked as local in %s namespace", s.Namespace), "failed validation")
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to get local storageconsumer in %s namespace", s.Namespace))
	}

	s.Log.Info("populating default parameters", "storageclass", storageClass.Name, "provisioner", storageClass.Provisioner)
	var provisionerSecretName, nodeStageSecretName string
	if storageClass.Provisioner == util.RbdDriverName {
		provisionerSecretName = getSecretName("rbd", "provisioner", clusterID)
		nodeStageSecretName = getSecretName("rbd", "node", clusterID)
	} else if storageClass.Provisioner == util.CephFSDriverName {
		provisionerSecretName = getSecretName("cephfs", "provisioner", clusterID)
		nodeStageSecretName = getSecretName("cephfs", "node", clusterID)
	}

	patches := []jsonpatch.JsonPatchOperation{}
	if len(storageClass.Parameters) != 0 {
		patches = append(patches, jsonpatch.JsonPatchOperation{
			Operation: "add",
			Path:      "/parameters",
			Value:     map[string]string{},
		})
		s.Log.Info("adding storageclass parameters section")
	}
	if storageClass.Parameters["csi.storage.k8s.io/provisioner-secret-name"] != "" {
		patches = append(patches, jsonpatch.JsonPatchOperation{
			Operation: "add",
			// forward slash (/) in json key should be replaced with (~1) as per RFC6901
			Path:  "/parameters/csi.storage.k8s.io~1provisioner-secret-name",
			Value: provisionerSecretName,
		})
		s.Log.Info("populating provisioner secret name in storageclass parameters section")
	}
	if storageClass.Parameters["csi.storage.k8s.io/node-stage-secret-name"] != "" {
		patches = append(patches, jsonpatch.JsonPatchOperation{
			Operation: "add",
			Path:      "/parameters/csi.storage.k8s.io~1node-stage-secret-name",
			Value:     nodeStageSecretName,
		})
		s.Log.Info("populating node stage secret name in storageclass parameters section")
	}
	if storageClass.Parameters["csi.storage.k8s.io/controller-expand-secret-name"] != "" {
		patches = append(patches, jsonpatch.JsonPatchOperation{
			Operation: "add",
			Path:      "/parameters/csi.storage.k8s.io~1controller-expand-secret-name",
			Value:     provisionerSecretName,
		})
		s.Log.Info("populating controller expand secret name in storageclass parameters section")
	}
	if storageClass.Parameters["clusterID"] != "" {
		patches = append(patches, jsonpatch.JsonPatchOperation{
			Operation: "add",
			Path:      "/parameters/clusterID",
			Value:     clusterID,
		})
		s.Log.Info("populating cluster id in storageclass parameters section")
	}

	return admission.Patched("setting default storageclass parameters if doesn't exist", patches...)
}

func getSecretName(storage, user, id string) string {
	return fmt.Sprintf("%s-%s-%s", storage, user, id)
}
