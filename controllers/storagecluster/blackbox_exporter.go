/*
Copyright 2025 Red Hat OpenShift Container Storage.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package storagecluster

import (
	"context"
	"fmt"
	"slices"

	securityv1 "github.com/openshift/api/security/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/version"
	"go.uber.org/multierr"
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
)

const (
	blackboxExporterName   = "odf-blackbox-exporter"
	blackboxServiceAccount = "odf-blackbox-exporter"
	blackboxSCCName        = "odf-blackbox-scc"
	blackboxConfigMapName  = "odf-blackbox-exporter-config"
	portBlackboxHTTP       = "http"
	blackboxScrapeInterval = "30s"
	blackboxPortNumber     = 9115
)

var blackboxExporterLabels = map[string]string{
	componentLabel: "blackbox-exporter",
	nameLabel:      blackboxExporterName,
	versionLabel:   version.Version,
}

// deployBlackboxExporter ensures the Blackbox Exporter is deployed by default
func (r *StorageClusterReconciler) deployBlackboxExporter(ctx context.Context, instance *ocsv1.StorageCluster) error {
	if instance.Spec.Monitoring != nil && instance.Spec.Monitoring.DisableBlackboxExporter {
		return r.deleteBlackboxExporter(ctx, instance)
	}

	if err := r.createBlackboxServiceAccount(ctx, instance); err != nil {
		r.Log.Error(err, "unable to create serviceaccount for blackbox metrics exporter")
		return err
	}
	if err := r.createBlackboxSCC(ctx, instance); err != nil {
		r.Log.Error(err, "unable to create SCC for blackbox metrics exporter")
		return err
	}
	if err := r.createBlackboxConfigMap(ctx, instance); err != nil {
		r.Log.Error(err, "unable to create configmap for blackbox metrics exporter")
		return err
	}
	if err := r.createBlackboxDeployment(ctx, instance); err != nil {
		r.Log.Error(err, "unable to create deployment for blackbox metrics exporter")
		return err
	}
	if err := r.createBlackboxService(ctx, instance); err != nil {
		r.Log.Error(err, "unable to create service for blackbox metrics exporter")
		return err
	}
	if err := r.createBlackboxProbe(ctx, instance); err != nil {
		r.Log.Error(err, "unable to create probe for blackbox metrics exporter")
		return err
	}
	return nil
}

// deleteBlackboxExporter removes all Blackbox Exporter components
func (r *StorageClusterReconciler) deleteBlackboxExporter(ctx context.Context, instance *ocsv1.StorageCluster) error {
	var finalErr error

	// Namespaced resources to delete using cached client
	resources := []client.Object{
		&monitoringv1.Probe{ObjectMeta: metav1.ObjectMeta{Name: blackboxExporterName, Namespace: instance.Namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: blackboxExporterName, Namespace: instance.Namespace}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: blackboxExporterName, Namespace: instance.Namespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: blackboxConfigMapName, Namespace: instance.Namespace}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: blackboxServiceAccount, Namespace: instance.Namespace}},
	}

	for _, obj := range resources {
		err := r.Client.Delete(ctx, obj)
		if err != nil && !apierrors.IsNotFound(err) {
			r.Log.Error(err, "Failed to delete Blackbox resource", "Kind", fmt.Sprintf("%T", obj), "Name", obj.GetName())
			multierr.AppendInto(&finalErr, err)
		} else if err == nil {
			r.Log.Info("Deleted Blackbox resource", "Kind", fmt.Sprintf("%T", obj), "Name", obj.GetName())
		}
	}

	// Delete cluster-scoped SCC using direct API client
	err := r.SecurityClient.SecurityContextConstraints().Delete(ctx, blackboxSCCName, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		r.Log.Error(err, "Failed to delete Blackbox SCC", "SCC", blackboxSCCName)
		multierr.AppendInto(&finalErr, err)
	} else if err == nil {
		r.Log.Info("Deleted Blackbox SCC", "SCC", blackboxSCCName)
	}

	return finalErr
}

// createBlackboxServiceAccount creates or updates the ServiceAccount
func (r *StorageClusterReconciler) createBlackboxServiceAccount(ctx context.Context, instance *ocsv1.StorageCluster) error {
	desired := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      blackboxServiceAccount,
			Namespace: instance.Namespace,
			Labels:    blackboxExporterLabels,
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

	err := r.Client.Get(ctx, types.NamespacedName{Name: actual.Name, Namespace: actual.Namespace}, actual)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return r.Client.Create(ctx, desired)
		}
		return err
	}

	// If ServiceAccount exists and Labels and OwnerReferences are correct, we don't need to update anything.
	if equality.Semantic.DeepEqual(desired.Labels, actual.Labels) &&
		equality.Semantic.DeepEqual(desired.OwnerReferences, actual.OwnerReferences) {
		return nil
	}
	// If ServiceAccount exists but Labels and/or OwnerReferences are incorrect, we need to update it.
	actual.Labels = desired.Labels
	actual.OwnerReferences = desired.OwnerReferences
	err = r.Client.Update(ctx, actual)
	return err

}

// createBlackboxSCC creates or updates the SCC
func (r *StorageClusterReconciler) createBlackboxSCC(ctx context.Context, instance *ocsv1.StorageCluster) error {
	desired := &securityv1.SecurityContextConstraints{
		ObjectMeta: metav1.ObjectMeta{
			Name: blackboxSCCName,
			Labels: map[string]string{
				nameLabel: blackboxExporterName,
			},
		},
		AllowPrivilegedContainer: false,
		RunAsUser: securityv1.RunAsUserStrategyOptions{
			Type: securityv1.RunAsUserStrategyMustRunAsRange,
		},
		SELinuxContext: securityv1.SELinuxContextStrategyOptions{
			Type: securityv1.SELinuxStrategyMustRunAs,
		},
		FSGroup: securityv1.FSGroupStrategyOptions{
			Type: securityv1.FSGroupStrategyMustRunAs,
		},
		SupplementalGroups: securityv1.SupplementalGroupsStrategyOptions{
			Type: securityv1.SupplementalGroupsStrategyRunAsAny,
		},
		Volumes: []securityv1.FSType{
			securityv1.FSTypeConfigMap,
			securityv1.FSTypeSecret,
			securityv1.FSTypeDownwardAPI,
			securityv1.FSTypeEmptyDir,
		},
		Users: []string{
			fmt.Sprintf("system:serviceaccount:%s:%s", instance.Namespace, blackboxServiceAccount),
		},
		AllowedCapabilities: []corev1.Capability{"NET_RAW"},
		Priority:            ptr.To[int32](10),
	}
	actual, err := r.SecurityClient.SecurityContextConstraints().Get(ctx, desired.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			if _, err := r.SecurityClient.SecurityContextConstraints().Create(ctx, desired, metav1.CreateOptions{}); err != nil {
				r.Log.Error(err, "Failed to create SCC", "SCC", desired.Name)
				return err
			}
			r.Log.Info("Created SCC", "SCC", desired.Name)
			return nil
		}
		r.Log.Error(err, "Failed to get SCC", "SCC", desired.Name)
		return err
	}

	needsUpdate := false

	if !equality.Semantic.DeepEqual(desired.Labels, actual.Labels) {
		actual.Labels = desired.Labels
		needsUpdate = true
	}
	if !equality.Semantic.DeepEqual(desired.AllowedCapabilities, actual.AllowedCapabilities) {
		actual.AllowedCapabilities = desired.AllowedCapabilities
		needsUpdate = true
	}
	if actual.RunAsUser.Type != desired.RunAsUser.Type ||
		(desired.RunAsUser.UID != nil && (actual.RunAsUser.UID == nil || *actual.RunAsUser.UID != *desired.RunAsUser.UID)) {
		actual.RunAsUser = desired.RunAsUser
		needsUpdate = true
	}
	if desired.Priority != nil && (actual.Priority == nil || *actual.Priority != *desired.Priority) {
		actual.Priority = desired.Priority
		needsUpdate = true
	}
	if actual.AllowPrivilegedContainer != desired.AllowPrivilegedContainer {
		actual.AllowPrivilegedContainer = desired.AllowPrivilegedContainer
		needsUpdate = true
	}
	if !equality.Semantic.DeepEqual(desired.Volumes, actual.Volumes) {
		actual.Volumes = desired.Volumes
		needsUpdate = true
	}
	// Preserve existing users/groups
	actual.Users = mergeStringSlices(actual.Users, desired.Users)
	actual.Groups = mergeStringSlices(actual.Groups, desired.Groups)

	if needsUpdate {
		// Note: Cannot set owner reference on cluster-scoped SCC from namespaced StorageCluster
		// SCC cleanup is handled by deleteBlackboxExporter()
		if _, err := r.SecurityClient.SecurityContextConstraints().Update(ctx, actual, metav1.UpdateOptions{}); err != nil {
			r.Log.Error(err, "Failed to update SCC", "SCC", actual.Name)
			return err
		}
		r.Log.Info("Updated SCC", "SCC", actual.Name)
	}

	return nil
}

func mergeStringSlices(existing, desired []string) []string {
	result := make([]string, len(existing))
	copy(result, existing)
	for _, item := range desired {
		if !slices.Contains(result, item) {
			result = append(result, item)
		}
	}
	return result
}

// createBlackboxConfigMap creates or updates the ConfigMap
func (r *StorageClusterReconciler) createBlackboxConfigMap(ctx context.Context, instance *ocsv1.StorageCluster) error {

	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      blackboxConfigMapName,
			Namespace: instance.Namespace,
			Labels:    blackboxExporterLabels,
		},
		Data: map[string]string{
			"config.yml": `
modules:
  icmp_internal:
    prober: icmp
    timeout: 5s
    icmp:
      preferred_ip_protocol: ip4
`,
		},
	}

	actual := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, actual, func() error {
		if actual.CreationTimestamp.IsZero() {
			actual.Labels = desired.Labels
			if err := controllerutil.SetControllerReference(instance, actual, r.Scheme); err != nil {
				return err
			}
		}
		actual.Data = desired.Data
		return nil
	})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		r.Log.Error(err, "Failed to create/update ConfigMap for Blackbox Exporter")
		return err
	}

	r.Log.V(1).Info("ConfigMap for Blackbox Exporter is ready")
	return nil
}

func (r *StorageClusterReconciler) getNodeIPs(ctx context.Context) ([]string, error) {
	nodes := &corev1.NodeList{}
	err := r.Client.List(ctx, nodes, client.MatchingLabels{defaults.NodeAffinityKey: ""})
	if err != nil {
		return nil, err
	}

	var ips []string
	for _, node := range nodes.Items {
		for _, addr := range node.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				ips = append(ips, addr.Address)
				break
			}
		}
	}
	return ips, nil
}

// createBlackboxDeployment deploys the Blackbox Exporter
func (r *StorageClusterReconciler) createBlackboxDeployment(ctx context.Context, instance *ocsv1.StorageCluster) error {
	desired := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      blackboxExporterName,
			Namespace: instance.Namespace,
			Labels:    blackboxExporterLabels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: blackboxExporterLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: blackboxExporterLabels,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: blackboxServiceAccount,
					SecurityContext: &corev1.PodSecurityContext{
						Sysctls: []corev1.Sysctl{
							{
								Name:  "net.ipv4.ping_group_range",
								Value: "0 2147483647",
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  blackboxExporterName,
							Image: r.images.BlackboxExporter,
							Args: []string{
								"--config.file=/etc/blackbox_exporter/config.yml",
								"--web.listen-address=:9115",
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          portBlackboxHTTP,
									ContainerPort: blackboxPortNumber,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config",
									MountPath: "/etc/blackbox_exporter",
									ReadOnly:  true,
								},
							},
							SecurityContext: &corev1.SecurityContext{
								RunAsNonRoot: ptr.To(true),
								RunAsUser:    ptr.To(int64(65534)),
								Capabilities: &corev1.Capabilities{
									Add: []corev1.Capability{"NET_RAW"},
								},
								AllowPrivilegeEscalation: ptr.To(false),
								SeccompProfile: &corev1.SeccompProfile{
									Type: corev1.SeccompProfileTypeRuntimeDefault,
								},
							},
							Resources: getDaemonResources(blackboxExporterName, instance),
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: blackboxConfigMapName,
									},
								},
							},
						},
					},
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

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, actual, func() error {
		if actual.CreationTimestamp.IsZero() {
			actual.Spec.Selector = &metav1.LabelSelector{MatchLabels: blackboxExporterLabels}
			if err := controllerutil.SetControllerReference(instance, actual, r.Scheme); err != nil {
				return err
			}
		}
		actual.Spec.Template = desired.Spec.Template
		return nil
	})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		r.Log.Error(err, "Failed to create/update Deployment for Blackbox Exporter")
		return err
	}

	return nil
}

// createBlackboxService exposes the exporter
func (r *StorageClusterReconciler) createBlackboxService(ctx context.Context, instance *ocsv1.StorageCluster) error {
	expected := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      blackboxExporterName,
			Namespace: instance.Namespace,
			Labels:    blackboxExporterLabels,
		},
		Spec: corev1.ServiceSpec{
			Selector: blackboxExporterLabels,
			Ports: []corev1.ServicePort{
				{
					Name:       portBlackboxHTTP,
					Port:       blackboxPortNumber,
					TargetPort: intstr.FromInt(blackboxPortNumber),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
	actual := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      expected.Name,
			Namespace: expected.Namespace,
		},
	}

	err := r.Client.Get(ctx, types.NamespacedName{Name: actual.Name, Namespace: actual.Namespace}, actual)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create new Service
			if err := controllerutil.SetControllerReference(instance, expected, r.Scheme); err != nil {
				r.Log.Error(err, "Failed to set owner reference on Service", "Service", expected.Name)
				return err
			}
			if err := r.Client.Create(ctx, expected); err != nil {
				r.Log.Error(err, "Failed to create Service", "Service", expected.Name)
				return err
			}
			r.Log.Info("Created Service", "Service", expected.Name)
			return nil
		}
		r.Log.Error(err, "Failed to get Service", "Service", expected.Name)
		return err
	}

	// Preserve ClusterIP across updates
	if actual.Spec.ClusterIP != "" {
		expected.Spec.ClusterIP = actual.Spec.ClusterIP
	}

	// Only update if the labels or spec differ
	if !equality.Semantic.DeepEqual(expected.Labels, actual.Labels) ||
		!equality.Semantic.DeepEqual(expected.Spec, actual.Spec) {

		actual.Labels = expected.Labels
		actual.Spec = expected.Spec

		if err := controllerutil.SetControllerReference(instance, actual, r.Scheme); err != nil {
			r.Log.Error(err, "Failed to set owner reference on Service", "Service", actual.Name)
			return err
		}
		if err := r.Client.Update(ctx, actual); err != nil {
			r.Log.Error(err, "Failed to update Service", "Service", actual.Name)
			return err
		}
		r.Log.Info("Updated Service", "Service", actual.Name)
	}

	return nil
}

// createBlackboxProbe creates or updates the Probe
func (r *StorageClusterReconciler) createBlackboxProbe(ctx context.Context, instance *ocsv1.StorageCluster) error {
	nodeIPs, err := r.getNodeIPs(ctx)
	if err != nil {
		r.Log.Error(err, "Failed to get node IPs")
		return err
	}

	desired := &monitoringv1.Probe{
		ObjectMeta: metav1.ObjectMeta{
			Name:      blackboxExporterName,
			Namespace: instance.Namespace,
			Labels:    blackboxExporterLabels,
		},
		Spec: monitoringv1.ProbeSpec{
			ProberSpec: monitoringv1.ProberSpec{
				URL: fmt.Sprintf("%s.%s.svc:%d", blackboxExporterName, instance.Namespace, blackboxPortNumber),
			},
			Module: "icmp_internal",
			Targets: monitoringv1.ProbeTargets{
				StaticConfig: &monitoringv1.ProbeTargetStaticConfig{
					Targets: nodeIPs,
					Labels: map[string]string{
						"job": blackboxExporterName,
					},
				},
			},
			Interval:      blackboxScrapeInterval,
			ScrapeTimeout: "5s",
		},
	}
	actual := &monitoringv1.Probe{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, actual, func() error {
		if actual.CreationTimestamp.IsZero() {
			actual.Spec = desired.Spec
			if err := controllerutil.SetControllerReference(instance, actual, r.Scheme); err != nil {
				return err
			}
		} else {
			actual.Spec = desired.Spec
		}
		return nil
	})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		r.Log.Error(err, "Failed to create/update Probe for Blackbox Exporter")
		return err
	}

	return nil
}
