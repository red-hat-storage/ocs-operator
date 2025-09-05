package storagecluster

import (
	"context"
	"fmt"

	"github.com/imdario/mergo"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/version"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	controllerutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	metricsExporterName     = "ocs-metrics-exporter"
	prometheusRoleName      = "ocs-metrics-svc"
	metricsExporterRoleName = metricsExporterName
	portMetricsMain         = "https-main"
	portMetricsSelf         = "https-self"
	metricsPath             = "/metrics"
	rbdMirrorMetricsPath    = "/metrics/rbd-mirror"
	scrapeInterval          = "1m"

	componentLabel = "app.kubernetes.io/component"
	nameLabel      = "app.kubernetes.io/name"
	versionLabel   = "app.kubernetes.io/version"
)

var exporterLabels = map[string]string{
	componentLabel: metricsExporterName,
	nameLabel:      metricsExporterName,
}

// enableMetricsExporter function start metrics exporter deployment
// and needed services
func (r *StorageClusterReconciler) enableMetricsExporter(
	ctx context.Context, instance *ocsv1.StorageCluster) error {
	// create the needed serviceaccount
	if err := createMetricsExporterServiceAccount(ctx, r, instance); err != nil {
		r.Log.Error(err, "unable to create serviceaccount for ocs metrics exporter")
		return err
	}

	// create/update clusterrole for metrics exporter
	if err := updateMetricsExporterClusterRoles(ctx, r); err != nil {
		r.Log.Error(err, "unable to update clusterroles for metrics exporter")
		return err
	}

	// create/update the cluster wide role-bindings for the above serviceaccount
	if err := updateMetricsExporterClusterRoleBindings(ctx, r); err != nil {
		r.Log.Error(err, "unable to update rolebindings for metrics exporter")
		return err
	}

	// create/update the namespace wise roles
	if err := createMetricsExporterRoles(ctx, r, instance); err != nil {
		r.Log.Error(err, "failed to create/update roles for metrics exporter")
		return err
	}

	// create/update the rolebindings for the above roles
	if err := createMetricsExporterRolebindings(ctx, r, instance); err != nil {
		r.Log.Error(err, "failed to create/update rolebindings for metrics exporter")
		return err
	}

	// create/update rook-ceph monitoring rolebindings
	if err := createRookCephClusterRolebindings(ctx, r, instance); err != nil {
		return err
	}

	// create/update the config-map needed for the exporter deployment
	if err := createMetricsExporterConfigMap(ctx, r, instance); err != nil {
		r.Log.Error(err, "failed to create configmap for metrics exporter")
		return err
	}

	// create/update the secret needed for the exporter deployment
	if err := createMetricsExporterSecret(ctx, r, instance); err != nil {
		r.Log.Error(err, "failed to create secret for metrics exporter")
		return err
	}

	// create the metrics exporter deployment
	if err := deployMetricsExporter(ctx, r, instance); err != nil {
		r.Log.Error(err, "failed to create ocs-metric-exporter deployment")
		return err
	}

	// start the exporter service
	_, err := createMetricsExporterService(ctx, r, instance)
	if err != nil {
		return err
	}
	// add the servicemonitor
	_, err = createMetricsExporterServiceMonitor(ctx, r, instance)
	if err != nil {
		return err
	}

	// create a `ocs-metrics-exporter-ceph-auth` secret for metrics exporter
	// create a cephclient and it will create the secret
	// and create ceph clients if external storage is not enabled
	if !instance.Spec.ExternalStorage.Enable {
		err = r.createMetricsExporterCephClient(instance)
		if err != nil {
			r.Log.Error(err, "Failed to create ceph client for metrics exporter.")
			return err
		}
	}

	return nil
}

func getMetricsExporterService(instance *ocsv1.StorageCluster) *corev1.Service {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      metricsExporterName,
			Namespace: instance.Namespace,
			Labels:    exporterLabels,
			Annotations: map[string]string{
				"service.beta.openshift.io/serving-cert-secret-name": "ocs-metrics-exporter-tls",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: instance.APIVersion,
					Kind:       instance.Kind,
					Name:       instance.Name,
					UID:        instance.UID,
				},
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:     portMetricsMain,
					Port:     int32(8443),
					Protocol: corev1.ProtocolTCP,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: int32(8443),
						StrVal: "8443",
					},
				},

				{
					Name:     portMetricsSelf,
					Port:     int32(9443),
					Protocol: corev1.ProtocolTCP,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: int32(9443),
						StrVal: "9443",
					},
				},
			},
			Selector: exporterLabels,
		},
	}
	return service
}

// createMetricsExporterService creates service object or an error
func createMetricsExporterService(ctx context.Context, r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (*corev1.Service, error) {
	service := getMetricsExporterService(instance)
	namespacedName := types.NamespacedName{Namespace: service.GetNamespace(), Name: service.GetName()}

	r.Log.Info("Reconciling metrics exporter service", "NamespacedName", namespacedName)

	oldService := &corev1.Service{}
	err := r.Client.Get(ctx, namespacedName, oldService)
	if err != nil {
		if apierrors.IsNotFound(err) {
			err = r.Client.Create(ctx, service)
			if err != nil {
				return nil, fmt.Errorf("failed to create metrics exporter service %v. %v", namespacedName, err)
			}
			return service, nil
		}
		return nil, fmt.Errorf("failed to retrieve metrics exporter service %v. %v", namespacedName, err)
	}
	service.ResourceVersion = oldService.ResourceVersion
	service.Spec.ClusterIP = oldService.Spec.ClusterIP
	err = r.Client.Update(ctx, service)
	if err != nil {
		return nil, fmt.Errorf("failed to update service %v. %v", namespacedName, err)
	}
	return service, nil
}

func getMetricsExporterServiceMonitor(instance *ocsv1.StorageCluster) *monitoringv1.ServiceMonitor {
	// Make a copy of the exporterLabels. Because we use exporterLabels in multiple places
	// (labels and selector for the ocs-metrics-exporter service, as well as service monitor),
	// changing the value of labels of a service monitor affects all of them.
	// Because this is the only place where we need to make a change, create a new copy here.
	serviceMonitorLabels := map[string]string{}
	for key, val := range exporterLabels {
		serviceMonitorLabels[key] = val
	}

	serverName := fmt.Sprintf("ocs-metrics-exporter.%s.svc", instance.GetNamespace())

	// To add storagecluster CR name to the metrics as label: managedBy
	relabelConfigs := []monitoringv1.RelabelConfig{
		{
			TargetLabel: "managedBy",
			Replacement: ptr.To(instance.Name),
		},
	}

	serviceMonitor := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      metricsExporterName,
			Namespace: instance.Namespace,
			Labels:    serviceMonitorLabels,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: instance.APIVersion,
				Kind:       instance.Kind,
				Name:       instance.Name,
				UID:        instance.UID,
			}},
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{instance.Namespace},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: exporterLabels,
			},
			Endpoints: []monitoringv1.Endpoint{
				{
					BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
					Interval:        scrapeInterval,
					Port:            portMetricsMain,
					Path:            metricsPath,
					RelabelConfigs:  relabelConfigs,
					Scheme:          "https",
					TLSConfig: &monitoringv1.TLSConfig{
						SafeTLSConfig: monitoringv1.SafeTLSConfig{
							InsecureSkipVerify: ptr.To(false),
							ServerName:         ptr.To(serverName),
						},
						CAFile: "/etc/prometheus/configmaps/serving-certs-ca-bundle/service-ca.crt",
					},
				},
				{
					BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
					Interval:        scrapeInterval,
					Port:            portMetricsMain,
					Path:            rbdMirrorMetricsPath,
					RelabelConfigs:  relabelConfigs,
					Scheme:          "https",
					TLSConfig: &monitoringv1.TLSConfig{
						SafeTLSConfig: monitoringv1.SafeTLSConfig{
							InsecureSkipVerify: ptr.To(false),
							ServerName:         ptr.To(serverName),
						},
						CAFile: "/etc/prometheus/configmaps/serving-certs-ca-bundle/service-ca.crt",
					},
				},
				{
					BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
					Interval:        scrapeInterval,
					Port:            portMetricsSelf,
					Path:            metricsPath,
					RelabelConfigs:  relabelConfigs,
					Scheme:          "https",
					TLSConfig: &monitoringv1.TLSConfig{
						SafeTLSConfig: monitoringv1.SafeTLSConfig{
							InsecureSkipVerify: ptr.To(false),
							ServerName:         ptr.To(serverName),
						},
						CAFile: "/etc/prometheus/configmaps/serving-certs-ca-bundle/service-ca.crt",
					},
				},
			},
		},
	}
	return serviceMonitor
}

// createMetricsExporterServiceMonitor creates serviceMonitor object or an error
func createMetricsExporterServiceMonitor(ctx context.Context, r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (*monitoringv1.ServiceMonitor, error) {
	serviceMonitor := getMetricsExporterServiceMonitor(instance)
	namespacedName := types.NamespacedName{Name: serviceMonitor.Name, Namespace: serviceMonitor.Namespace}

	err := mergo.Merge(&serviceMonitor.Labels, instance.Spec.Monitoring.Labels, mergo.WithOverride)
	if err != nil {
		return nil, err
	}

	r.Log.Info("Reconciling metrics exporter service monitor", "NamespacedName", namespacedName)

	oldSm := &monitoringv1.ServiceMonitor{}
	err = r.Client.Get(ctx, namespacedName, oldSm)
	if err != nil {
		if apierrors.IsNotFound(err) {
			err = r.Client.Create(ctx, serviceMonitor)
			if err != nil {
				return nil, fmt.Errorf("failed to create metrics exporter servicemonitor %v. %v", namespacedName, err)
			}
			return serviceMonitor, nil
		}
		return nil, fmt.Errorf("failed to retrieve metrics exporter servicemonitor %v. %v", namespacedName, err)
	}
	oldSm.Spec = serviceMonitor.Spec

	err = mergo.Merge(&oldSm.Labels, serviceMonitor.Labels, mergo.WithOverride)
	if err != nil {
		return nil, err
	}

	err = r.Client.Update(ctx, oldSm)
	if err != nil {
		return nil, fmt.Errorf("failed to update metrics exporter servicemonitor %v. %v", namespacedName, err)
	}
	return serviceMonitor, nil
}

func deployMetricsExporter(ctx context.Context, r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {
	currentDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      metricsExporterName,
			Namespace: instance.Namespace,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, currentDep, func() error {
		if currentDep.ObjectMeta.CreationTimestamp.IsZero() {
			// Selector is immutable. Inject it only while creating new object.
			currentDep.Spec.Selector = &metav1.LabelSelector{
				MatchLabels: exporterLabels,
			}
		}

		currentDep.ObjectMeta = metav1.ObjectMeta{
			Name:      metricsExporterName,
			Namespace: instance.Namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         instance.APIVersion,
				BlockOwnerDeletion: ptr.To(false),
				Controller:         ptr.To(false),
				Kind:               instance.Kind,
				Name:               instance.Name,
				UID:                instance.UID,
			}},
			Labels: map[string]string{
				componentLabel: exporterLabels[componentLabel],
				nameLabel:      exporterLabels[nameLabel],
				versionLabel:   version.Version,
			},
		}

		currentDep.Spec.Template = corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					componentLabel: exporterLabels[componentLabel],
					nameLabel:      exporterLabels[nameLabel],
					versionLabel:   version.Version,
				},
			},
			Spec: corev1.PodSpec{
				SecurityContext: &corev1.PodSecurityContext{
					RunAsNonRoot: ptr.To(true),
				},
				HostNetwork: shouldUseHostNetworking(instance),
				Containers: []corev1.Container{
					{
						Resources: defaults.MonitoringResources["kube-rbac-proxy"],
						Name:      "kube-rbac-proxy-main",
						SecurityContext: &corev1.SecurityContext{
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
							},
							AllowPrivilegeEscalation: ptr.To(false),
						},
						Ports: []corev1.ContainerPort{
							{
								Name:          "https-main",
								ContainerPort: 8443,
								Protocol:      corev1.ProtocolTCP,
							},
						},
						Args: []string{
							"--secure-listen-address", "0.0.0.0:8443",
							"--upstream", "http://127.0.0.1:8080/",
							"--tls-cert-file", "/etc/tls/private/tls.crt",
							"--tls-private-key-file", "/etc/tls/private/tls.key",
							"--tls-cipher-suites", "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305",
							"--config-file", "/etc/kube-rbac-policy/config.yaml",
							"--v", "10",
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "ocs-metrics-exporter-tls",
								MountPath: "/etc/tls/private",
								ReadOnly:  true,
							},
							{
								Name:      "ocs-metrics-exporter-kube-rbac-proxy-config",
								MountPath: "/etc/kube-rbac-policy",
								ReadOnly:  true,
							},
						},
					},
					{
						Resources: defaults.MonitoringResources["kube-rbac-proxy"],
						Name:      "kube-rbac-proxy-self",
						SecurityContext: &corev1.SecurityContext{
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
							},
							AllowPrivilegeEscalation: ptr.To(false),
						},
						Ports: []corev1.ContainerPort{
							{
								Name:          "https-self",
								ContainerPort: 9443,
								Protocol:      corev1.ProtocolTCP,
							},
						},
						Args: []string{
							"--secure-listen-address", "0.0.0.0:9443",
							"--upstream", "http://127.0.0.1:8081/",
							"--tls-cert-file", "/etc/tls/private/tls.crt",
							"--tls-private-key-file", "/etc/tls/private/tls.key",
							"--tls-cipher-suites", "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305",
							"--config-file", "/etc/kube-rbac-policy/config.yaml",
							"--v", "10",
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "ocs-metrics-exporter-tls",
								MountPath: "/etc/tls/private",
								ReadOnly:  true,
							},
							{
								Name:      "ocs-metrics-exporter-kube-rbac-proxy-config",
								MountPath: "/etc/kube-rbac-policy",
								ReadOnly:  true,
							},
						},
					},
					{
						Args: []string{
							"--namespaces", instance.Namespace,
							"--ceph-auth-namespace", r.OperatorNamespace,
						},
						Command: []string{"/usr/local/bin/metrics-exporter"},
						Image:   r.images.OCSMetricsExporter,
						Name:    metricsExporterName,
						LivenessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path:   "/healthz",
									Port:   intstr.FromInt32(8080),
									Scheme: corev1.URISchemeHTTP,
								},
							},
							InitialDelaySeconds: 15,
						},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path:   "/metrics",
									Port:   intstr.FromInt32(8080),
									Scheme: corev1.URISchemeHTTP,
								},
							},
							InitialDelaySeconds: 15,
						},
						Resources: getDaemonResources("ocs-metrics-exporter", instance),
						SecurityContext: &corev1.SecurityContext{
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
							},
							RunAsNonRoot:             ptr.To(true),
							ReadOnlyRootFilesystem:   ptr.To(true),
							Privileged:               ptr.To(false),
							AllowPrivilegeEscalation: ptr.To(false),
							SeccompProfile: &corev1.SeccompProfile{
								Type: corev1.SeccompProfileTypeRuntimeDefault,
							},
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "ceph-config",
							MountPath: "/etc/ceph",
							ReadOnly:  true,
						}},
					},
				},
				PriorityClassName:  systemClusterCritical,
				ServiceAccountName: metricsExporterName,
				Volumes: []corev1.Volume{
					{
						Name: "ceph-config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "ocs-metrics-exporter-ceph-conf",
								},
							},
						},
					},
					{
						Name: "ocs-metrics-exporter-tls",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "ocs-metrics-exporter-tls",
							},
						},
					},
					{
						Name: "ocs-metrics-exporter-kube-rbac-proxy-config",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "ocs-metrics-exporter-kube-rbac-proxy-config",
							},
						},
					},
				},
				Tolerations: getPlacement(instance, defaults.MetricsExporterKey).Tolerations,
				Affinity: &corev1.Affinity{
					NodeAffinity: getPlacement(instance, defaults.MetricsExporterKey).NodeAffinity,
				},
			},
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func createMetricsExporterServiceAccount(ctx context.Context, r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {
	expectedServiceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      metricsExporterName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				componentLabel: exporterLabels[componentLabel],
				nameLabel:      exporterLabels[nameLabel],
				versionLabel:   version.Version,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: instance.APIVersion,
				Kind:       instance.Kind,
				Name:       instance.Name,
				UID:        instance.UID,
			}},
		},
	}

	// We only care about the existence of this ServiceAccount and presence of correct Labels and OwnerReferences in it.
	// We do not want to reset/remove Secret references or ImagePullSecret references set by the system controllers.
	currentServiceAccount := &corev1.ServiceAccount{}
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: metricsExporterName}, currentServiceAccount)
	if err != nil {
		if apierrors.IsNotFound(err) {
			err = r.Client.Create(ctx, expectedServiceAccount)
			return err
		}
		return err
	}
	// If ServiceAccount exists and Labels and OwnerReferences are correct, we don't need to update anything.
	if equality.Semantic.DeepEqual(expectedServiceAccount.ObjectMeta.Labels, currentServiceAccount.ObjectMeta.Labels) &&
		equality.Semantic.DeepEqual(expectedServiceAccount.ObjectMeta.OwnerReferences, currentServiceAccount.ObjectMeta.OwnerReferences) {
		return nil
	}
	// If ServiceAccount exists but Labels and/or OwnerReferences are incorrect, we need to update it.
	currentServiceAccount.ObjectMeta.Labels = expectedServiceAccount.ObjectMeta.Labels
	currentServiceAccount.ObjectMeta.OwnerReferences = expectedServiceAccount.ObjectMeta.OwnerReferences
	err = r.Client.Update(ctx, currentServiceAccount)
	return err
}

// updateMetricsExporterClusterRoleBindings function updates the cluster level rolebindings for the metrics exporter in this namespace
func updateMetricsExporterClusterRoleBindings(ctx context.Context, r *StorageClusterReconciler) error {
	currentCRB := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: metricsExporterName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, currentCRB, func() error {
		if currentCRB.CreationTimestamp.IsZero() {
			currentCRB.RoleRef = rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     metricsExporterName,
			}
		}

		currentCRB.ObjectMeta = metav1.ObjectMeta{
			Name: metricsExporterName,
			Labels: map[string]string{
				componentLabel: exporterLabels[componentLabel],
				nameLabel:      exporterLabels[nameLabel],
				versionLabel:   version.Version,
			},
		}

		currentCRB.Subjects = []rbacv1.Subject{}
		for _, ns := range r.clusters.GetNamespaces() {
			subject := rbacv1.Subject{
				Kind:      "ServiceAccount",
				Name:      metricsExporterName,
				Namespace: ns,
			}
			currentCRB.Subjects = append(currentCRB.Subjects, subject)
		}

		return nil
	})

	return err
}

func createMetricsExporterConfigMap(ctx context.Context, r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {
	currentConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-metrics-exporter-ceph-conf",
			Namespace: instance.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, currentConfigMap, func() error {
		currentConfigMap.ObjectMeta = metav1.ObjectMeta{
			Name:      "ocs-metrics-exporter-ceph-conf",
			Namespace: instance.Namespace,
			Labels: map[string]string{
				componentLabel: exporterLabels[componentLabel],
				nameLabel:      exporterLabels[nameLabel],
				versionLabel:   version.Version,
			},
		}

		currentConfigMap.Data = map[string]string{
			"ceph.conf": `
[global]
auth_cluster_required = cephx
auth_service_required = cephx
auth_client_required = cephx
`,
			// keyring is a required key and its value should be empty
			"keyring": "",
		}

		return nil
	})

	return err
}

func createMetricsExporterSecret(ctx context.Context, r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {
	currentSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-metrics-exporter-kube-rbac-proxy-config",
			Namespace: instance.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, currentSecret, func() error {
		currentSecret.ObjectMeta = metav1.ObjectMeta{
			Name:      "ocs-metrics-exporter-kube-rbac-proxy-config",
			Namespace: instance.Namespace,
			Labels: map[string]string{
				componentLabel: exporterLabels[componentLabel],
				nameLabel:      exporterLabels[nameLabel],
				versionLabel:   version.Version,
			},
		}
		currentSecret.StringData = map[string]string{
			"config.yaml": `
"authorization":
  "static":
  - "path": "/metrics"
    "resourceRequest": false
    "user":
        "name": "system:serviceaccount:openshift-monitoring:prometheus-k8s"
    "verb": "get"
  - "path": "/metrics/rbd-mirror"
    "resourceRequest": false
    "user":
        "name": "system:serviceaccount:openshift-monitoring:prometheus-k8s"
    "verb": "get"
`,
		}
		return nil
	})

	return err
}

func updateMetricsExporterClusterRoles(ctx context.Context, r *StorageClusterReconciler) error {
	currentClusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: metricsExporterName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, currentClusterRole, func() error {
		currentClusterRole.ObjectMeta = metav1.ObjectMeta{
			Name: metricsExporterName,
			Labels: map[string]string{
				componentLabel: exporterLabels[componentLabel],
				nameLabel:      exporterLabels[nameLabel],
				versionLabel:   version.Version,
			},
		}

		currentClusterRole.Rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{"authentication.k8s.io"},
				Resources: []string{"tokenreviews"},
				Verbs:     []string{"create"},
			},
			{
				APIGroups: []string{"authorization.k8s.io"},
				Resources: []string{"subjectaccessreviews"},
				Verbs:     []string{"create"},
			},
			{
				APIGroups:     []string{""},
				Resources:     []string{"configmaps"},
				Verbs:         []string{"get"},
				ResourceNames: []string{"rook-ceph-csi-config"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"persistentvolumes", "persistentvolumeclaims", "pods", "nodes"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"quota.openshift.io"},
				Resources: []string{"clusterresourcequotas"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"storage.k8s.io"},
				Resources: []string{"storageclasses"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"objectbucket.io"},
				Resources: []string{"objectbuckets"},
				Verbs:     []string{"get", "list"},
			},
		}

		return nil
	})

	if err != nil {
		return err
	}

	currentMetricsReaderClusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocs-metrics-reader",
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, currentMetricsReaderClusterRole, func() error {
		currentClusterRole.ObjectMeta = metav1.ObjectMeta{
			Name: "ocs-metrics-reader",
			Labels: map[string]string{
				componentLabel: exporterLabels[componentLabel],
				nameLabel:      exporterLabels[nameLabel],
				versionLabel:   version.Version,
			},
		}

		currentClusterRole.Rules = []rbacv1.PolicyRule{
			{
				NonResourceURLs: []string{"/metrics", "/metrics/rbd-mirror", "/healthz"},
				Verbs:           []string{"get"},
			},
		}

		return nil
	})

	return err
}

func createMetricsExporterRoles(ctx context.Context, r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {

	currentPrometheusK8sRole := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      prometheusRoleName,
			Namespace: instance.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, currentPrometheusK8sRole, func() error {
		currentPrometheusK8sRole.ObjectMeta = metav1.ObjectMeta{
			Name:      prometheusRoleName,
			Namespace: instance.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: instance.APIVersion,
					Kind:       instance.Kind,
					Name:       instance.Name,
					UID:        instance.UID,
				},
			},
			Labels: map[string]string{
				componentLabel: exporterLabels[componentLabel],
				nameLabel:      exporterLabels[nameLabel],
				versionLabel:   version.Version,
			},
		}

		currentPrometheusK8sRole.Rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"services", "endpoints", "pods"},
				Verbs:     []string{"get", "list", "watch"},
			},
		}

		return nil
	})

	if err != nil {
		return err
	}

	currentMetricExporterRole := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      metricsExporterRoleName,
			Namespace: instance.Namespace,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, currentMetricExporterRole, func() error {
		currentMetricExporterRole.ObjectMeta = metav1.ObjectMeta{
			Name:      metricsExporterRoleName,
			Namespace: instance.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: instance.APIVersion,
					Kind:       instance.Kind,
					Name:       instance.Name,
					UID:        instance.UID,
				},
			},
			Labels: map[string]string{
				componentLabel: exporterLabels[componentLabel],
				nameLabel:      exporterLabels[nameLabel],
				versionLabel:   version.Version,
			},
		}

		currentMetricExporterRole.Rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"secrets", "configmaps"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"monitoring.coreos.com"},
				Resources: []string{"prometheusrules", "servicemonitors"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "delete"},
			},
			{
				APIGroups: []string{"ceph.rook.io"},
				Resources: []string{"cephobjectstores", "cephclusters", "cephblockpools", "cephrbdmirrors", "cephblockpoolradosnamespaces"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"objectbucket.io"},
				Resources: []string{"objectbucketclaims"},
				Verbs:     []string{"get", "list"},
			},
			{
				APIGroups: []string{"ocs.openshift.io"},
				Resources: []string{
					"storageconsumers",
					"storageclusters",
					"storageautoscalers",
				},
				Verbs: []string{"get", "list", "watch"},
			},
		}

		return nil
	})

	if err != nil {
		return err
	}

	return nil
}

func createMetricsExporterRolebindings(ctx context.Context, r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {
	currentPrometheusK8RoleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      prometheusRoleName,
			Namespace: instance.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, currentPrometheusK8RoleBinding, func() error {
		if currentPrometheusK8RoleBinding.CreationTimestamp.IsZero() {
			// RoleRef is immutable. So inject it only while creating new object.
			currentPrometheusK8RoleBinding.RoleRef = rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "Role",
				Name:     prometheusRoleName,
			}
		}

		currentPrometheusK8RoleBinding.ObjectMeta = metav1.ObjectMeta{
			Name:      prometheusRoleName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				componentLabel: exporterLabels[componentLabel],
				nameLabel:      exporterLabels[nameLabel],
				versionLabel:   version.Version,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: instance.APIVersion,
				Kind:       instance.Kind,
				Name:       instance.Name,
				UID:        instance.UID,
			}},
		}

		currentPrometheusK8RoleBinding.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "prometheus-k8s",
				Namespace: "openshift-monitoring",
			},
		}

		return nil
	})

	if err != nil {
		return err
	}

	currentMetricsExporterRoleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      metricsExporterRoleName,
			Namespace: instance.Namespace,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, currentMetricsExporterRoleBinding, func() error {
		if currentMetricsExporterRoleBinding.CreationTimestamp.IsZero() {
			// RoleRef is immutable. So inject it only while creating new object.
			currentMetricsExporterRoleBinding.RoleRef = rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "Role",
				Name:     metricsExporterRoleName,
			}
		}

		currentMetricsExporterRoleBinding.ObjectMeta = metav1.ObjectMeta{
			Name:      metricsExporterRoleName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				componentLabel: exporterLabels[componentLabel],
				nameLabel:      exporterLabels[nameLabel],
				versionLabel:   version.Version,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: instance.APIVersion,
				Kind:       instance.Kind,
				Name:       instance.Name,
				UID:        instance.UID,
			}},
		}

		currentMetricsExporterRoleBinding.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      metricsExporterName,
				Namespace: instance.Namespace,
			},
		}

		return nil
	})

	if err != nil {
		return err
	}

	return nil
}

func createRookCephClusterRolebindings(ctx context.Context,
	r *StorageClusterReconciler, _ *ocsv1.StorageCluster) error {
	// rookCephMonitorMgrRoleBinding is a cluster rolebinding for monitor mgr
	rookCephMonitorMgrRoleBinding := rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "rook-ceph-monitor-mgr",
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "rook-ceph-monitor-mgr",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "rook-ceph-mgr",
				Namespace: r.OperatorNamespace,
			},
		},
	}

	// rookCephMonitorRoleBinding is a cluster rolebinding for rook-ceph monitor
	rookCephMonitorRoleBinding := rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "rook-ceph-monitor",
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "rook-ceph-monitor",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "rook-ceph-system",
				Namespace: r.OperatorNamespace,
			},
		},
	}

	var roleBindings = []rbacv1.ClusterRoleBinding{
		rookCephMonitorMgrRoleBinding, rookCephMonitorRoleBinding,
	}

	for _, expectedClusterRoleBinding := range roleBindings {
		currentClusterRoleBinding := new(rbacv1.ClusterRoleBinding)
		expectedClusterRoleBinding.ObjectMeta.DeepCopyInto(&currentClusterRoleBinding.ObjectMeta)

		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, currentClusterRoleBinding, func() error {
			// add expected role reference
			if currentClusterRoleBinding.CreationTimestamp.IsZero() {
				// RoleRef is immutable. So inject it only while creating new object.
				currentClusterRoleBinding.RoleRef = expectedClusterRoleBinding.RoleRef
			}

			// add expected subjects
			currentClusterRoleBinding.Subjects = expectedClusterRoleBinding.Subjects
			return nil
		})
		if err != nil {
			r.Log.Error(err,
				"error while create/update rook ceph rolebinding",
				"RoleBindingName", expectedClusterRoleBinding.Name,
			)
			return err
		}
	}

	return nil
}

func (r *StorageClusterReconciler) createMetricsExporterCephClient(instance *ocsv1.StorageCluster) error {
	cephClient := &rookCephv1.CephClient{}
	cephClient.Name = util.OcsMetricsExporterCephClientName
	cephClient.Namespace = instance.Namespace

	if _, err := ctrl.CreateOrUpdate(r.ctx, r.Client, cephClient, func() error {
		if err := controllerutil.SetControllerReference(instance, cephClient, r.Scheme); err != nil {
			return err
		}
		cephClient.Spec.SecretName = cephClient.Name
		cephClient.Spec.Caps = map[string]string{
			"mon": "profile rbd, allow command 'osd blocklist'",
			"mgr": "allow rw",
			"osd": "profile rbd",
			"mds": "allow *",
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (r *StorageClusterReconciler) deleteMetricsExporterCephClient(namespace string) error {
	cephClient := &rookCephv1.CephClient{}
	cephClient.Name = util.OcsMetricsExporterCephClientName
	cephClient.Namespace = namespace

	if err := r.Client.Delete(r.ctx, cephClient); err != nil {
		if apierrors.IsNotFound(err) {
			// If the CephClient does not exist, we can safely return nil.
			return nil
		}

		return fmt.Errorf("failed to delete CephClient %v. %v", cephClient.Name, err)
	}

	return nil
}
