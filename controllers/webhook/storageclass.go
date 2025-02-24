package webhook

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"

	"github.com/go-logr/logr"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	jsonpatch "gomodules.xyz/jsonpatch/v2"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	util.NfsDriverName,
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

	storageCluster, err := util.GetStorageClusterInNamespace(ctx, s.Client, s.Namespace)
	if err != nil {
		s.Log.Error(err, "failed to list storagecluster", "namespace", s.Namespace)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to get storagecluster in %s namespace", s.Namespace))
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

	var localConsumer *v1alpha1.StorageConsumer
	for idx := range storageConsumerList.Items {
		consumer := &storageConsumerList.Items[idx]
		controller := metav1.GetControllerOfNoCopy(consumer)
		if controller != nil && controller.UID == storageCluster.UID {
			localConsumer = consumer
			break
		}
	}
	if localConsumer == nil {
		s.Log.Error(fmt.Errorf("no storageconsumer found with storagecluster as the owner"), "failed validation")
		return admission.Denied("creation of storageclass should happen after existence of local storageconsumer")
	}

	if localConsumer.Status.ResourceNameMappingConfigMap.Name == "" {
		s.Log.Info("local storageconsumer doesn't have resource configmap populated yet", "StorageConsumer", localConsumer.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("awaiting local storageconsumer to have resources mapped"))
	}
	resourceConfigMap := &corev1.ConfigMap{}
	resourceConfigMap.Name = localConsumer.Status.ResourceNameMappingConfigMap.Name
	resourceConfigMap.Namespace = localConsumer.Namespace
	if err := s.Client.Get(ctx, client.ObjectKeyFromObject(resourceConfigMap), resourceConfigMap); err != nil {
		s.Log.Error(err, "failed to get configmap", "ConfigMap", resourceConfigMap.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to get configmap %s", resourceConfigMap.Name))
	}

	s.Log.Info("populating default parameters", "StorageClass", storageClass.Name, "provisioner", storageClass.Provisioner)
	res := util.WrapStorageConsumerResourceMap(resourceConfigMap.Data)
	patches := []jsonpatch.JsonPatchOperation{}
	if len(storageClass.Parameters) != 0 {
		// TODO: we are expecting StorageClass is partially filled, we should not be in this block
		patches = append(patches, jsonpatch.JsonPatchOperation{
			Operation: "add",
			Path:      "/parameters",
			Value:     map[string]string{},
		})
	}
	switch storageClass.Provisioner {
	case util.RbdDriverName:
		appendPatchesForProvisioner(
			&patches,
			res.GetRbdClientProfileName(),
			res.GetCsiRbdProvisionerSecretName(),
			res.GetCsiRbdNodeSecretName(),
			s.Namespace,
		)
	case util.CephFSDriverName:
		appendPatchesForProvisioner(
			&patches,
			res.GetCephFsClientProfileName(),
			res.GetCsiCephFsProvisionerSecretName(),
			res.GetCsiCephFsNodeSecretName(),
			s.Namespace,
		)
	case util.NfsDriverName:
		appendPatchesForProvisioner(
			&patches,
			res.GetNfsClientProfileName(),
			res.GetCsiNfsProvisionerSecretName(),
			res.GetCsiNfsNodeSecretName(),
			s.Namespace,
		)
	}
	return admission.Patched("setting default storageclass parameters if doesn't exist", patches...)
}

func appendPatchesForProvisioner(
	patches *[]jsonpatch.JsonPatchOperation,
	clusterID, provisionerSecretName, nodeSecretName, namespace string,
) {
	*patches = append(*patches, jsonpatch.JsonPatchOperation{
		Operation: "add",
		Path:      "/parameters/clusterID",
		Value:     clusterID,
	})

	*patches = append(*patches, jsonpatch.JsonPatchOperation{
		Operation: "add",
		// forward slash (/) in json key should be replaced with (~1) as per RFC6901
		Path:  "/parameters/csi.storage.k8s.io~1provisioner-secret-name",
		Value: provisionerSecretName,
	})
	*patches = append(*patches, jsonpatch.JsonPatchOperation{
		Operation: "add",
		Path:      "/parameters/csi.storage.k8s.io~1provisioner-secret-namespace",
		Value:     namespace,
	})

	*patches = append(*patches, jsonpatch.JsonPatchOperation{
		Operation: "add",
		Path:      "/parameters/csi.storage.k8s.io~1node-stage-secret-name",
		Value:     nodeSecretName,
	})
	*patches = append(*patches, jsonpatch.JsonPatchOperation{
		Operation: "add",
		Path:      "/parameters/csi.storage.k8s.io~1node-stage-secret-namespace",
		Value:     namespace,
	})

	*patches = append(*patches, jsonpatch.JsonPatchOperation{
		Operation: "add",
		Path:      "/parameters/csi.storage.k8s.io~1controller-expand-secret-name",
		Value:     provisionerSecretName,
	})
	*patches = append(*patches, jsonpatch.JsonPatchOperation{
		Operation: "add",
		Path:      "/parameters/csi.storage.k8s.io~1controller-expand-secret-namespace",
		Value:     namespace,
	})
}
