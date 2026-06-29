package storagecluster

import (
	"fmt"
	"strings"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	vaultAgentDefaultRole          = "rook-ceph-rgw"
	vaultAgentDefaultAuthMountPath = "auth/kubernetes"
	vaultAgentConfigFile           = "vault-agent-config.hcl"
	vaultAgentTLSCAMountPath       = "/vault/tls/ca"
	vaultAgentTLSCertMountPath     = "/vault/tls/client-cert"
	vaultAgentTLSKeyMountPath      = "/vault/tls/client-key"
	vaultAgentTokenPath            = "/vault/token"
)

var vaultAgentLabels = map[string]string{
	"app": VaultAgentDeploymentName,
}

type ocsVaultAgent struct{}

func (obj *ocsVaultAgent) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	required, err := obj.isVaultAgentRequired(r, instance)
	if err != nil {
		r.Log.Error(err, "Failed to determine if Vault Agent is required")
		return reconcile.Result{}, err
	}
	if !required {
		return obj.ensureDeleted(r, instance)
	}

	if r.images.VaultAgent == "" {
		r.Log.Info("VAULT_AGENT_IMAGE not set, skipping Vault Agent deployment for RGW SSE-S3")
		return reconcile.Result{}, nil
	}

	kmsConfigMap, err := util.GetKMSConfigMap(defaults.KMSConfigMapName, instance, r.Client)
	if err != nil {
		r.Log.Error(err, "Failed to get KMS ConfigMap for Vault Agent")
		return reconcile.Result{}, err
	}
	if kmsConfigMap == nil {
		return reconcile.Result{}, nil
	}

	if err := r.createVaultAgentServiceAccount(instance); err != nil {
		r.Log.Error(err, "Failed to create Vault Agent ServiceAccount")
		return reconcile.Result{}, err
	}
	if err := r.createVaultAgentConfigMap(instance, kmsConfigMap); err != nil {
		r.Log.Error(err, "Failed to create Vault Agent ConfigMap")
		return reconcile.Result{}, err
	}
	if err := r.createVaultAgentDeployment(instance, kmsConfigMap); err != nil {
		r.Log.Error(err, "Failed to create Vault Agent Deployment")
		return reconcile.Result{}, err
	}
	if err := r.createVaultAgentService(instance); err != nil {
		r.Log.Error(err, "Failed to create Vault Agent Service")
		return reconcile.Result{}, err
	}

	r.Log.Info("Vault Agent resources reconciled successfully")
	return reconcile.Result{}, nil
}

func (obj *ocsVaultAgent) ensureDeleted(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	resources := []client.Object{
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: VaultAgentServiceName, Namespace: instance.Namespace}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: VaultAgentDeploymentName, Namespace: instance.Namespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: VaultAgentConfigMapName, Namespace: instance.Namespace}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: VaultAgentSAName, Namespace: instance.Namespace}},
	}

	for _, obj := range resources {
		err := r.Delete(r.ctx, obj)
		if err != nil && !apierrors.IsNotFound(err) {
			r.Log.Error(err, "Failed to delete Vault Agent resource", "Kind", fmt.Sprintf("%T", obj), "Name", obj.GetName())
			return reconcile.Result{}, err
		}
	}
	return reconcile.Result{}, nil
}

// isVaultAgentRequired checks whether the Vault Agent deployment is needed.
// It returns true only when all of the following are satisfied:
//   - Cluster or device-set encryption is enabled
//   - KMS is enabled
//   - KMS provider is vault (or empty, which defaults to vault)
//   - VAULT_RGW_AUTH_METHOD is set to "agent"
func (obj *ocsVaultAgent) isVaultAgentRequired(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (bool, error) {
	if util.IsClusterOrDeviceSetEncrypted(instance) && instance.Spec.Encryption.KeyManagementService.Enable {
		kmsConfigMap, err := util.GetKMSConfigMap(defaults.KMSConfigMapName, instance, r.Client)
		if err != nil {
			return false, err
		}
		if kmsConfigMap == nil {
			return false, nil
		}
		// Empty provider defaults to vault, so only reject if explicitly non-vault
		kmsProvider := kmsConfigMap.Data[KMSProviderKey]
		if kmsProvider != "" && kmsProvider != VaultKMSProvider {
			return false, nil
		}
		return kmsConfigMap.Data[VaultRGWAuthMethodKey] == VaultAgentAuthMethod, nil
	}
	return false, nil
}

func (r *StorageClusterReconciler) createVaultAgentServiceAccount(instance *ocsv1.StorageCluster) error {
	desired := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      VaultAgentSAName,
			Namespace: instance.Namespace,
			Labels:    vaultAgentLabels,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: instance.APIVersion,
				Kind:       instance.Kind,
				Name:       instance.Name,
				UID:        instance.UID,
			}},
		},
	}
	actual := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		},
	}

	err := r.Get(r.ctx, types.NamespacedName{Name: actual.Name, Namespace: actual.Namespace}, actual)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return r.Create(r.ctx, desired)
		}
		return err
	}

	if equality.Semantic.DeepEqual(desired.Labels, actual.Labels) &&
		equality.Semantic.DeepEqual(desired.OwnerReferences, actual.OwnerReferences) {
		return nil
	}
	actual.Labels = desired.Labels
	actual.OwnerReferences = desired.OwnerReferences
	return r.Update(r.ctx, actual)
}

func (r *StorageClusterReconciler) createVaultAgentConfigMap(instance *ocsv1.StorageCluster, kmsConfigMap *corev1.ConfigMap) error {
	hclConfig := generateVaultAgentConfig(kmsConfigMap)

	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      VaultAgentConfigMapName,
			Namespace: instance.Namespace,
			Labels:    vaultAgentLabels,
		},
		Data: map[string]string{
			vaultAgentConfigFile: hclConfig,
		},
	}

	actual := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, actual, func() error {
		if actual.CreationTimestamp.IsZero() {
			actual.Labels = desired.Labels
			if err := controllerutil.SetControllerReference(instance, actual, r.Scheme); err != nil {
				return err
			}
		}
		actual.Data = desired.Data
		return nil
	})
	return err
}

func (r *StorageClusterReconciler) createVaultAgentDeployment(instance *ocsv1.StorageCluster, kmsConfigMap *corev1.ConfigMap) error {
	volumes, volumeMounts := getVaultAgentTLSVolumes(kmsConfigMap)

	// Always need the config volume and token volume
	volumes = append(volumes,
		corev1.Volume{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: VaultAgentConfigMapName,
					},
				},
			},
		},
		corev1.Volume{
			Name: "token",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium: corev1.StorageMediumMemory,
				},
			},
		},
	)
	volumeMounts = append(volumeMounts,
		corev1.VolumeMount{
			Name:      "config",
			MountPath: "/etc/vault",
			ReadOnly:  true,
		},
		corev1.VolumeMount{
			Name:      "token",
			MountPath: vaultAgentTokenPath,
		},
	)

	desired := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      VaultAgentDeploymentName,
			Namespace: instance.Namespace,
			Labels:    vaultAgentLabels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(2)),
			Selector: &metav1.LabelSelector{
				MatchLabels: vaultAgentLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: vaultAgentLabels,
					Annotations: util.RequiredSCCAnnotation(util.SCCRestrictedV2),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: VaultAgentSAName,
					Containers: []corev1.Container{
						{
							Name:    "vault-agent",
							Image:   r.images.VaultAgent,
							Command: []string{"vault"},
							Args: []string{
								"agent",
								"-config=/etc/vault/" + vaultAgentConfigFile,
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "cache",
									ContainerPort: int32(VaultAgentPort),
									Protocol:      corev1.ProtocolTCP,
								},
							},
							VolumeMounts: volumeMounts,
							Resources:    getDaemonResources("vault-agent", instance),
							SecurityContext: &corev1.SecurityContext{
								RunAsNonRoot:             ptr.To(true),
								ReadOnlyRootFilesystem:   ptr.To(true),
								AllowPrivilegeEscalation: ptr.To(false),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
								SeccompProfile: &corev1.SeccompProfile{
									Type: corev1.SeccompProfileTypeRuntimeDefault,
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.FromInt(VaultAgentPort),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       15,
								FailureThreshold:    6,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.FromInt(VaultAgentPort),
									},
								},
								InitialDelaySeconds: 15,
								PeriodSeconds:       20,
								FailureThreshold:    6,
							},
						},
					},
					Volumes:           volumes,
					PriorityClassName: systemClusterCritical,
				},
			},
		},
	}

	actual := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, actual, func() error {
		if actual.CreationTimestamp.IsZero() {
			actual.Spec.Selector = desired.Spec.Selector
			if err := controllerutil.SetControllerReference(instance, actual, r.Scheme); err != nil {
				return err
			}
		}
		actual.Spec.Replicas = desired.Spec.Replicas
		actual.Spec.Template = desired.Spec.Template
		return nil
	})
	return err
}

func (r *StorageClusterReconciler) createVaultAgentService(instance *ocsv1.StorageCluster) error {
	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      VaultAgentServiceName,
			Namespace: instance.Namespace,
			Labels:    vaultAgentLabels,
		},
		Spec: corev1.ServiceSpec{
			Selector: vaultAgentLabels,
			Ports: []corev1.ServicePort{
				{
					Name:       "cache",
					Port:       int32(VaultAgentPort),
					TargetPort: intstr.FromInt(VaultAgentPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	actual := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		},
	}

	err := r.Get(r.ctx, types.NamespacedName{Name: actual.Name, Namespace: actual.Namespace}, actual)
	if err != nil {
		if apierrors.IsNotFound(err) {
			if err := controllerutil.SetControllerReference(instance, desired, r.Scheme); err != nil {
				return err
			}
			return r.Create(r.ctx, desired)
		}
		return err
	}

	// Preserve ClusterIP across updates
	if actual.Spec.ClusterIP != "" {
		desired.Spec.ClusterIP = actual.Spec.ClusterIP
	}

	if !equality.Semantic.DeepEqual(desired.Labels, actual.Labels) ||
		!equality.Semantic.DeepEqual(desired.Spec.Selector, actual.Spec.Selector) ||
		!equality.Semantic.DeepEqual(desired.Spec.Ports, actual.Spec.Ports) {
		actual.Labels = desired.Labels
		actual.Spec.Selector = desired.Spec.Selector
		actual.Spec.Ports = desired.Spec.Ports
		if err := controllerutil.SetControllerReference(instance, actual, r.Scheme); err != nil {
			return err
		}
		return r.Update(r.ctx, actual)
	}
	return nil
}

// generateVaultAgentConfig creates the Vault Agent HCL configuration from the KMS ConfigMap
func generateVaultAgentConfig(kmsConfigMap *corev1.ConfigMap) string {
	vaultAddr := kmsConfigMap.Data["VAULT_ADDR"]
	role := kmsConfigMap.Data[VaultRGWRoleKey]
	if role == "" {
		role = vaultAgentDefaultRole
	}
	authMountPath := kmsConfigMap.Data[VaultRGWAuthMountPathKey]
	if authMountPath == "" {
		authMountPath = vaultAgentDefaultAuthMountPath
	}

	tlsBlock := generateVaultAgentTLSConfig(kmsConfigMap)

	return fmt.Sprintf(`disable_mlock = true

vault {
  address = %q
%s}

auto_auth {
  method "kubernetes" {
    mount_path = %q
    config = {
      role = %q
    }
  }
  sink "file" {
    config = {
      path = "%s/.vault-token"
    }
  }
}

cache {
  use_auto_auth_token = true
}

listener "tcp" {
  address     = "0.0.0.0:%d"
  tls_disable = true
}
`, vaultAddr, tlsBlock, authMountPath, role, vaultAgentTokenPath, VaultAgentPort)
}

// generateVaultAgentTLSConfig generates the tls_config block for the Vault Agent HCL
func generateVaultAgentTLSConfig(kmsConfigMap *corev1.ConfigMap) string {
	var lines []string
	if s := kmsConfigMap.Data["VAULT_CACERT"]; s != "" {
		lines = append(lines, fmt.Sprintf("    ca_cert     = %q", vaultAgentTLSCAMountPath+"/cert"))
	}
	if s := kmsConfigMap.Data["VAULT_CLIENT_CERT"]; s != "" {
		lines = append(lines, fmt.Sprintf("    client_cert = %q", vaultAgentTLSCertMountPath+"/cert"))
	}
	if s := kmsConfigMap.Data["VAULT_CLIENT_KEY"]; s != "" {
		lines = append(lines, fmt.Sprintf("    client_key  = %q", vaultAgentTLSKeyMountPath+"/key"))
	}
	if len(lines) == 0 {
		return ""
	}
	return fmt.Sprintf("  tls_config {\n%s\n  }\n", strings.Join(lines, "\n"))
}

// getVaultAgentTLSVolumes returns volumes and volume mounts for TLS secrets
func getVaultAgentTLSVolumes(kmsConfigMap *corev1.ConfigMap) ([]corev1.Volume, []corev1.VolumeMount) {
	var volumes []corev1.Volume
	var mounts []corev1.VolumeMount

	type tlsVolumeSpec struct {
		configKey string
		name      string
		mountPath string
	}

	specs := []tlsVolumeSpec{
		{"VAULT_CACERT", "vault-tls-ca", vaultAgentTLSCAMountPath},
		{"VAULT_CLIENT_CERT", "vault-tls-client-cert", vaultAgentTLSCertMountPath},
		{"VAULT_CLIENT_KEY", "vault-tls-client-key", vaultAgentTLSKeyMountPath},
	}

	for _, spec := range specs {
		secretName := kmsConfigMap.Data[spec.configKey]
		if secretName == "" {
			continue
		}
		volumes = append(volumes, corev1.Volume{
			Name: spec.name,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secretName,
				},
			},
		})
		mounts = append(mounts, corev1.VolumeMount{
			Name:      spec.name,
			MountPath: spec.mountPath,
			ReadOnly:  true,
		})
	}

	return volumes, mounts
}

// VaultAgentServiceURL returns the in-cluster URL for the Vault Agent service
func VaultAgentServiceURL(namespace string) string {
	return fmt.Sprintf("http://%s.%s.svc:%d", VaultAgentServiceName, namespace, VaultAgentPort)
}
