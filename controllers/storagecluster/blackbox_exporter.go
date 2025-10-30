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
	"os"

	"github.com/imdario/mergo"
	securityv1 "github.com/openshift/api/security/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/version"
	"go.uber.org/multierr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	blackboxExporterName      = "odf-blackbox-exporter"
	blackboxExporterNamespace = "openshift-storage"
	blackboxServiceAccount    = "odf-blackbox-exporter"
	blackboxSCCName           = "odf-blackbox-scc"
	blackboxConfigMapName     = "odf-blackbox-exporter-config"
	blackboxSCCRoleName       = "odf-blackbox-scc-role"
	blackboxSCCBindingName    = "odf-blackbox-scc-binding"
	portBlackboxHTTP          = "http"
	pathMetrics               = "/metrics"
	blackboxScrapeInterval    = "30s"
)

var blackboxExporterLabels = map[string]string{
	componentLabel: "blackbox-exporter",
	nameLabel:      blackboxExporterName,
	versionLabel:   version.Version,
}

// enableBlackboxExporter ensures the Blackbox Exporter is deployed when enabled
func (r *StorageClusterReconciler) enableBlackboxExporter(ctx context.Context, instance *ocsv1.StorageCluster) error {
	if instance.Spec.Monitoring == nil || !instance.Spec.Monitoring.EnableBlackboxExporter {
		return r.deleteBlackboxExporter(ctx)
	}

	if err := r.createBlackboxServiceAccount(ctx, instance); err != nil {
		r.Log.Error(err, "unable to create serviceaccount for blackbox metrics exporter")
		return err
	}
	if err := r.createBlackboxSCC(ctx, instance); err != nil {
		r.Log.Error(err, "unable to create SCC for blackbox metrics exporter")
		return err
	}
	if err := r.createBlackboxSCCRole(ctx); err != nil {
		r.Log.Error(err, "unable to create ClusterRole for blackbox exporter SCC access")
		return err
	}
	if err := r.createBlackboxSCCBinding(ctx); err != nil {
		r.Log.Error(err, "unable to create ClusterRoleBinding for blackbox exporter SCC access")
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
	if err := r.createBlackboxServiceMonitor(ctx, instance); err != nil {
		r.Log.Error(err, "unable to create servicemonitor for blackbox metrics exporter")
		return err
	}
	return nil
}

// deleteBlackboxExporter removes all Blackbox Exporter components
func (r *StorageClusterReconciler) deleteBlackboxExporter(ctx context.Context) error {
	var finalErr error

	resources := []client.Object{
		&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Name: blackboxExporterName, Namespace: blackboxExporterNamespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: blackboxExporterName, Namespace: blackboxExporterNamespace}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: blackboxExporterName, Namespace: blackboxExporterNamespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: blackboxConfigMapName, Namespace: blackboxExporterNamespace}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: blackboxServiceAccount, Namespace: blackboxExporterNamespace}},
		&securityv1.SecurityContextConstraints{ObjectMeta: metav1.ObjectMeta{Name: blackboxSCCName}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: blackboxSCCRoleName}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: blackboxSCCBindingName}},
	}

	for _, obj := range resources {
		err := r.Client.Delete(ctx, obj)
		if err != nil && !errors.IsNotFound(err) {
			r.Log.Error(err, "Failed to delete Blackbox resource", "Kind", fmt.Sprintf("%T", obj), "Name", obj.GetName())
			multierr.AppendInto(&finalErr, err)
		} else if err == nil {
			r.Log.Info("Deleted Blackbox resource", "Kind", fmt.Sprintf("%T", obj), "Name", obj.GetName())
		}
	}

	return finalErr
}

// createBlackboxServiceAccount creates or updates the ServiceAccount
func (r *StorageClusterReconciler) createBlackboxServiceAccount(ctx context.Context, instance *ocsv1.StorageCluster) error {
	desired := r.getDesiredBlackboxServiceAccount(instance)
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

func (r *StorageClusterReconciler) getDesiredBlackboxServiceAccount(instance *ocsv1.StorageCluster) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      blackboxServiceAccount,
			Namespace: blackboxExporterNamespace,
			Labels:    blackboxExporterLabels,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: instance.APIVersion,
				Kind:       instance.Kind,
				Name:       instance.Name,
				UID:        instance.UID,
			}},
		},
	}
}

// createBlackboxSCC creates or updates the SCC
func (r *StorageClusterReconciler) createBlackboxSCC(ctx context.Context, instance *ocsv1.StorageCluster) error {
	desired := r.getDesiredBlackboxSCC()
	actual := &securityv1.SecurityContextConstraints{
		ObjectMeta: metav1.ObjectMeta{
			Name: desired.Name,
		},
	}

	err := r.Client.Get(ctx, types.NamespacedName{Name: actual.Name}, actual)
	if err != nil {
		if apierrors.IsNotFound(err) {
			if err := r.Client.Create(ctx, desired); err != nil {
				r.Log.Error(err, "Failed to create SCC", "SCC", desired.Name)
				return err
			}
			r.Log.Info("Created SCC", "SCC", desired.Name)
			return nil
		}
		r.Log.Error(err, "Failed to get SCC", "SCC", desired.Name)
		return err
	}

	// Only update if the labels differ
	if !equality.Semantic.DeepEqual(desired.Labels, actual.Labels) {

		actual.Labels = desired.Labels
		if err := controllerutil.SetControllerReference(instance, actual, r.Scheme); err != nil {
			r.Log.Error(err, "Failed to set owner reference on SCC", "SCC", actual.Name)
			return err
		}

		// Preserve existing users/groups
		actual.Users = mergeStringSlices(actual.Users, desired.Users)
		actual.Groups = mergeStringSlices(actual.Groups, desired.Groups)

		if err := r.Client.Update(ctx, actual); err != nil {
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
		if !containsString(result, item) {
			result = append(result, item)
		}
	}
	return result
}

func containsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func (r *StorageClusterReconciler) getDesiredBlackboxSCC() *securityv1.SecurityContextConstraints {
	return &securityv1.SecurityContextConstraints{
		ObjectMeta: metav1.ObjectMeta{
			Name: blackboxSCCName,
			Labels: map[string]string{
				nameLabel: blackboxExporterName,
			},
		},
		AllowPrivilegedContainer: false,
		RunAsUser: securityv1.RunAsUserStrategyOptions{
			Type: securityv1.RunAsUserStrategyMustRunAs,
			UID:  ptr.To[int64](0),
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
			fmt.Sprintf("system:serviceaccount:%s:%s", blackboxExporterNamespace, blackboxServiceAccount),
		},
		AllowedCapabilities: []corev1.Capability{"NET_RAW"},
		Priority:            ptr.To[int32](10),
	}
}

// createBlackboxSCCRole creates or updates the ClusterRole for SCC access
func (r *StorageClusterReconciler) createBlackboxSCCRole(ctx context.Context) error {
	desired := r.getDesiredBlackboxSCCRole()
	actual := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: desired.Name,
		},
	}

	err := r.Client.Get(ctx, types.NamespacedName{Name: actual.Name}, actual)
	if err != nil {
		if apierrors.IsNotFound(err) {
			if err := r.Client.Create(ctx, desired); err != nil {
				r.Log.Error(err, "Failed to create ClusterRole", "ClusterRole", desired.Name)
				return err
			}
			r.Log.Info("Created ClusterRole", "ClusterRole", desired.Name)
			return nil
		}
		r.Log.Error(err, "Failed to get ClusterRole", "ClusterRole", desired.Name)
		return err
	}
	if !equality.Semantic.DeepEqual(desired.Rules, actual.Rules) {
		actual.Rules = desired.Rules

		if err := r.Client.Update(ctx, actual); err != nil {
			r.Log.Error(err, "Failed to update ClusterRole", "ClusterRole", actual.Name)
			return err
		}
		r.Log.Info("Updated ClusterRole", "ClusterRole", actual.Name)
	}

	return nil
}

func (r *StorageClusterReconciler) getDesiredBlackboxSCCRole() *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: blackboxSCCRoleName,
			Labels: map[string]string{
				nameLabel: blackboxExporterName,
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"security.openshift.io"},
				Resources: []string{"securitycontextconstraints"},
				Verbs:     []string{"use"},
			},
		},
	}
}

// createBlackboxSCCBinding creates or updates the ClusterRoleBinding for SCC access
func (r *StorageClusterReconciler) createBlackboxSCCBinding(ctx context.Context) error {
	desired := r.getDesiredBlackboxSCCBinding()
	actual := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: desired.Name,
		},
	}

	err := r.Client.Get(ctx, types.NamespacedName{Name: actual.Name}, actual)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create new ClusterRoleBinding
			if err := r.Client.Create(ctx, desired); err != nil {
				r.Log.Error(err, "Failed to create ClusterRoleBinding", "ClusterRoleBinding", desired.Name)
				return err
			}
			r.Log.Info("Created ClusterRoleBinding", "ClusterRoleBinding", desired.Name)
			return nil
		}
		r.Log.Error(err, "Failed to get ClusterRoleBinding", "ClusterRoleBinding", desired.Name)
		return err
	}
	// Only update if our subjects or roleRef differ
	if !equality.Semantic.DeepEqual(desired.Subjects, actual.Subjects) ||
		!equality.Semantic.DeepEqual(desired.RoleRef, actual.RoleRef) {

		actual.Subjects = desired.Subjects
		actual.RoleRef = desired.RoleRef

		if err := r.Client.Update(ctx, actual); err != nil {
			r.Log.Error(err, "Failed to update ClusterRoleBinding", "ClusterRoleBinding", actual.Name)
			return err
		}
		r.Log.Info("Updated ClusterRoleBinding", "ClusterRoleBinding", actual.Name)
	}

	return nil
}

func (r *StorageClusterReconciler) getDesiredBlackboxSCCBinding() *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: blackboxSCCBindingName,
			Labels: map[string]string{
				nameLabel: blackboxExporterName,
			},
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      blackboxServiceAccount,
				Namespace: blackboxExporterNamespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind: "ClusterRole",
			Name: blackboxSCCRoleName,
		},
	}
}

// createBlackboxConfigMap creates or updates the ConfigMap
func (r *StorageClusterReconciler) createBlackboxConfigMap(ctx context.Context, instance *ocsv1.StorageCluster) error {

	desired := r.getDesiredBlackboxConfigMap(instance)
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
	if err != nil && !errors.IsAlreadyExists(err) {
		r.Log.Error(err, "Failed to create/update ConfigMap for Blackbox Exporter")
		return err
	}
	return nil
}

func (r *StorageClusterReconciler) getDesiredBlackboxConfigMap(instance *ocsv1.StorageCluster) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      blackboxConfigMapName,
			Namespace: blackboxExporterNamespace,
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
}

// createBlackboxDeployment deploys the Blackbox Exporter
func (r *StorageClusterReconciler) createBlackboxDeployment(ctx context.Context, instance *ocsv1.StorageCluster) error {
	desired := r.getDesiredBlackboxDeployment(instance)
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
	if err != nil && !errors.IsAlreadyExists(err) {
		r.Log.Error(err, "Failed to create/update Deployment for Blackbox Exporter")
		return err
	}

	r.Log.Info("Blackbox Exporter Deployment is ready")
	return nil
}

func (r *StorageClusterReconciler) getDesiredBlackboxDeployment(instance *ocsv1.StorageCluster) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      blackboxExporterName,
			Namespace: blackboxExporterNamespace,
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
					Containers: []corev1.Container{
						{
							Name:  blackboxExporterName,
							Image: os.Getenv("BLACKBOX_EXPORTER_IMAGE"),
							Args: []string{
								"--config.file=/etc/blackbox_exporter/config.yml",
								"--web.listen-address=:9115",
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          portBlackboxHTTP,
									ContainerPort: 9115,
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
								RunAsUser: ptr.To(int64(0)),
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
}

// createBlackboxService exposes the exporter
func (r *StorageClusterReconciler) createBlackboxService(ctx context.Context, instance *ocsv1.StorageCluster) error {
	expected := r.getDesiredBlackboxService(instance)
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

func (r *StorageClusterReconciler) getDesiredBlackboxService(instance *ocsv1.StorageCluster) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      blackboxExporterName,
			Namespace: blackboxExporterNamespace,
			Labels:    blackboxExporterLabels,
		},
		Spec: corev1.ServiceSpec{
			Selector: blackboxExporterLabels,
			Ports: []corev1.ServicePort{
				{
					Name:       portBlackboxHTTP,
					Port:       9115,
					TargetPort: intstr.FromInt(9115),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
}

// createBlackboxServiceMonitor enables Prometheus scraping
func (r *StorageClusterReconciler) createBlackboxServiceMonitor(ctx context.Context, instance *ocsv1.StorageCluster) error {
	expected := r.getDesiredBlackboxServiceMonitor(instance)
	actual := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      expected.Name,
			Namespace: expected.Namespace,
		},
	}

	err := r.Client.Get(ctx, types.NamespacedName{Name: actual.Name, Namespace: actual.Namespace}, actual)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create new ServiceMonitor
			if err := controllerutil.SetControllerReference(instance, expected, r.Scheme); err != nil {
				r.Log.Error(err, "Failed to set owner reference on ServiceMonitor", "ServiceMonitor", expected.Name)
				return err
			}
			if err := r.Client.Create(ctx, expected); err != nil {
				r.Log.Error(err, "Failed to create ServiceMonitor", "ServiceMonitor", expected.Name)
				return err
			}
			r.Log.Info("Created ServiceMonitor", "ServiceMonitor", expected.Name)
			return nil
		}
		r.Log.Error(err, "Failed to get ServiceMonitor", "ServiceMonitor", expected.Name)
		return err
	}

	// Only update if our labels or spec differ
	if !equality.Semantic.DeepEqual(expected.Labels, actual.Labels) ||
		!equality.Semantic.DeepEqual(expected.Spec, actual.Spec) {

		actual.Labels = expected.Labels
		actual.Spec = expected.Spec

		if err := controllerutil.SetControllerReference(instance, actual, r.Scheme); err != nil {
			r.Log.Error(err, "Failed to set owner reference on ServiceMonitor", "ServiceMonitor", actual.Name)
			return err
		}
		if err := r.Client.Update(ctx, actual); err != nil {
			r.Log.Error(err, "Failed to update ServiceMonitor", "ServiceMonitor", actual.Name)
			return err
		}
		r.Log.Info("Updated ServiceMonitor", "ServiceMonitor", actual.Name)
	}

	return nil
}

func (r *StorageClusterReconciler) getDesiredBlackboxServiceMonitor(instance *ocsv1.StorageCluster) *monitoringv1.ServiceMonitor {
	sm := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      blackboxExporterName,
			Namespace: blackboxExporterNamespace,
			Labels:    blackboxExporterLabels,
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			NamespaceSelector: monitoringv1.NamespaceSelector{MatchNames: []string{blackboxExporterNamespace}},
			Selector:          metav1.LabelSelector{MatchLabels: blackboxExporterLabels},
			Endpoints: []monitoringv1.Endpoint{
				{
					Port:     portBlackboxHTTP,
					Path:     pathMetrics,
					Interval: blackboxScrapeInterval,
				},
			},
		},
	}

	// Merge user-defined labels
	if instance.Spec.Monitoring != nil {
		err := mergo.Merge(&sm.Labels, instance.Spec.Monitoring.Labels, mergo.WithOverride)
		if err != nil {
			r.Log.Error(err, "Failed to merge user-defined labels into Service")
		}
	}
	return sm
}
