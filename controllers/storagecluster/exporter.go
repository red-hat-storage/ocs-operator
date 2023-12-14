package storagecluster

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/imdario/mergo"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/version"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	controllerutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	metricsExporterName     = "ocs-metrics-exporter"
	metricsExporterRoleName = "ocs-metrics-svc"
	portMetrics             = "metrics"
	portExporter            = "exporter"
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
	if err := updateMetricsExporterClusterRoleBindings(ctx, r, instance); err != nil {
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

	// create/update the config-map needed for the exporter deployment
	if err := createMetricsExporterConfigMap(ctx, r, instance); err != nil {
		r.Log.Error(err, "failed to create configmap for metrics exporter")
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
	return nil
}

func getMetricsExporterService(instance *ocsv1.StorageCluster) *corev1.Service {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      metricsExporterName,
			Namespace: instance.Namespace,
			Labels:    exporterLabels,
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
					Name:     portMetrics,
					Port:     int32(8080),
					Protocol: corev1.ProtocolTCP,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: int32(8080),
						StrVal: "8080",
					},
				},

				{
					Name:     portExporter,
					Port:     int32(8081),
					Protocol: corev1.ProtocolTCP,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: int32(8081),
						StrVal: "8081",
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

	// To add storagecluster CR name to the metrics as label: managedBy
	relabelConfigs := []*monitoringv1.RelabelConfig{
		{
			TargetLabel: "managedBy",
			Replacement: instance.Name,
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
					Port:           portMetrics,
					Path:           metricsPath,
					Interval:       scrapeInterval,
					RelabelConfigs: relabelConfigs,
				},
				{
					Port:           portMetrics,
					Path:           rbdMirrorMetricsPath,
					Interval:       scrapeInterval,
					RelabelConfigs: relabelConfigs,
				},
				{
					Port:     portExporter,
					Path:     metricsPath,
					Interval: scrapeInterval,
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

func getMetricExporterDeployment(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) *appsv1.Deployment {
	var (
		falsePtr                      = new(bool) // defaults to 'false'
		truePtr                       = new(bool)
		progressDeadlineSeconds int32 = 600
		replicas                int32 = 1
		revisionHistoryLimit    int32 = 1
		maxSurge                      = intstr.Parse("25%")
		maxUnavailable                = intstr.Parse("25%")
	)
	*truePtr = true
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      metricsExporterName,
			Namespace: instance.Namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         instance.APIVersion,
				BlockOwnerDeletion: falsePtr,
				Controller:         falsePtr,
				Kind:               instance.Kind,
				Name:               instance.Name,
				UID:                instance.UID,
			}},
			Labels: map[string]string{
				componentLabel: exporterLabels[componentLabel],
				nameLabel:      exporterLabels[nameLabel],
				versionLabel:   version.Version,
			},
		},
		Spec: appsv1.DeploymentSpec{
			ProgressDeadlineSeconds: &progressDeadlineSeconds,
			Replicas:                &replicas,
			RevisionHistoryLimit:    &revisionHistoryLimit,
			Selector: &metav1.LabelSelector{
				MatchLabels: exporterLabels,
			},
			Strategy: appsv1.DeploymentStrategy{
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxSurge:       &maxSurge,
					MaxUnavailable: &maxUnavailable,
				},
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						componentLabel: exporterLabels[componentLabel],
						nameLabel:      exporterLabels[nameLabel],
						versionLabel:   version.Version,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Args:    []string{"--namespaces", instance.Namespace},
						Command: []string{"/usr/local/bin/metrics-exporter"},
						Image:   r.images.OCSMetricsExporter,
						Name:    metricsExporterName,
						Ports: []corev1.ContainerPort{
							{ContainerPort: 8080},
							{ContainerPort: 8081},
						},
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot: truePtr,
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "ceph-config",
							MountPath: "/etc/ceph",
						}},
					}},
					ServiceAccountName: metricsExporterName,
					Volumes: []corev1.Volume{{
						Name: "ceph-config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "ocs-metrics-exporter-ceph-conf",
								},
							},
						},
					}},
				},
			},
		},
	}
	return dep
}

func deployMetricsExporter(ctx context.Context, r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {
	expectedDep := getMetricExporterDeployment(r, instance)
	namespacedName := types.NamespacedName{
		Name:      expectedDep.Name,
		Namespace: expectedDep.Namespace,
	}
	r.Log.Info("Deploying metrics exporter", "NamespacedName", namespacedName)

	currentDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      expectedDep.Name,
			Namespace: expectedDep.Namespace,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, currentDep, func() error {
		currentDep.ResourceVersion = expectedDep.ResourceVersion
		var currentImage string
		var currentOwner metav1.OwnerReference
		if len(currentDep.Spec.Template.Spec.Containers) > 0 {
			currentImage = currentDep.Spec.Template.Spec.Containers[0].Image
			// log if the images are different
			if currentImage != expectedDep.Spec.Template.Spec.Containers[0].Image {
				r.Log.Info("Different container image specified in the exporter deployment",
					"CurrentImage", currentImage,
					"DefaultImage", expectedDep.Spec.Template.Spec.Containers[0].Image)
			}
		} else {
			currentImage = expectedDep.Spec.Template.Spec.Containers[0].Image
		}
		if len(currentDep.OwnerReferences) > 0 {
			currentDep.OwnerReferences[0].DeepCopyInto(&currentOwner)
		} else {
			expectedDep.OwnerReferences[0].DeepCopyInto(&currentOwner)
		}
		if currentDep.Labels == nil {
			currentDep.Labels = make(map[string]string)
		}
		for lKey, lValue := range expectedDep.Labels {
			currentDep.Labels[lKey] = lValue
		}
		expectedDep.Spec.DeepCopyInto(&currentDep.Spec)
		if len(currentDep.OwnerReferences) == 0 {
			currentDep.OwnerReferences = make([]metav1.OwnerReference, 1)
		}
		// set the image and ownerref back to the current deployment
		currentDep.Spec.Template.Spec.Containers[0].Image = currentImage
		if currentOwner.APIVersion != "" && currentOwner.Kind != "" && currentOwner.Name != "" &&
			currentOwner.UID != "" {
			currentOwner.DeepCopyInto(&currentDep.OwnerReferences[0])
		}
		return nil
	}); err != nil {
		r.Log.Error(err, "failed to create/update metrics exporter deployment", "NamespacedName", namespacedName)
		return err
	}
	return nil
}

func createMetricsExporterServiceAccount(ctx context.Context, r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {
	expectedServiceAccount := corev1.ServiceAccount{
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
	currentServiceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      expectedServiceAccount.Name,
			Namespace: expectedServiceAccount.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, currentServiceAccount, func() error {
		if len(currentServiceAccount.OwnerReferences) == 0 {
			currentServiceAccount.OwnerReferences = make([]metav1.OwnerReference, 1)
		}
		currentOwner := &currentServiceAccount.OwnerReferences[0]
		// if any of the ownerref criteria is not satisfied,
		// set to the expected SA owenerref
		if currentOwner.APIVersion == "" || currentOwner.Kind == "" ||
			currentOwner.Name == "" || currentOwner.UID == "" {
			currentServiceAccount.OwnerReferences[0] = expectedServiceAccount.OwnerReferences[0]
		}
		return nil
	})
	return err
}

// updateMetricsExporterClusterRoleBindings function updates the
// cluster level rolebindings for the metrics exporter in this namespace
func updateMetricsExporterClusterRoleBindings(ctx context.Context,
	r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {
	currentCRB := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{
		Name: metricsExporterName,
	}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, currentCRB, func() error {
		currentCRB.RoleRef.Name = metricsExporterName
		currentCRB.RoleRef.Kind = "ClusterRole"
		currentCRB.RoleRef.APIGroup = "rbac.authorization.k8s.io"
		subjectNotFound := true
		expectedSubject := rbacv1.Subject{
			Kind:      "ServiceAccount",
			Name:      metricsExporterName,
			Namespace: instance.Namespace,
		}
		for _, subject := range currentCRB.Subjects {
			if subject.Kind == expectedSubject.Kind &&
				subject.Name == expectedSubject.Name &&
				subject.Namespace == expectedSubject.Namespace {
				subjectNotFound = false
				break
			}
		}
		if subjectNotFound {
			currentCRB.Subjects = append(currentCRB.Subjects, expectedSubject)
		}
		return nil
	})
	return err
}

func createMetricsExporterConfigMap(ctx context.Context,
	r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {
	configMapName := "ocs-metrics-exporter-ceph-conf"
	currentConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: instance.Namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: instance.APIVersion,
				Kind:       instance.Kind,
				Name:       instance.Name,
				UID:        instance.UID,
			}},
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, currentConfigMap, func() error {
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

const metricsExporterClusterRoleJSON = `
{
	"apiVersion":"rbac.authorization.k8s.io/v1",
	"kind":"ClusterRole",
	"metadata":{"name":"ocs-metrics-exporter"},
	"rules":[
		{
			"apiGroups":[""],
			"resources":["persistentvolumes","nodes"],
			"verbs":["get","list","watch"]
		},
		{
			"apiGroups":["quota.openshift.io"],
			"resources":["clusterresourcequotas"],
			"verbs":["get","list","watch"]
		},
		{
			"apiGroups":["storage.k8s.io"],
			"resources":["storageclasses"],
			"verbs":["get","list","watch"]
		},
		{
			"apiGroups":["objectbucket.io"],
			"resources":["objectbuckets"],
			"verbs":["get","list"]
		}
	]
}`

func updateMetricsExporterClusterRoles(ctx context.Context, r *StorageClusterReconciler) error {
	currentClusterRole := new(rbacv1.ClusterRole)
	var expectedClusterRole = new(rbacv1.ClusterRole)
	err := json.Unmarshal([]byte(metricsExporterClusterRoleJSON), expectedClusterRole)
	if err != nil {
		r.Log.Error(err, "an unexpected error occurred while unmarshalling clusterrole yaml")
		return err
	}
	expectedClusterRole.Name, currentClusterRole.Name = metricsExporterName, metricsExporterName
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, currentClusterRole, func() error {
		expectedRulesLen := len(expectedClusterRole.Rules)
		if len(currentClusterRole.Rules) != expectedRulesLen {
			currentClusterRole.Rules = make([]rbacv1.PolicyRule, expectedRulesLen)
		}
		copy(currentClusterRole.Rules, expectedClusterRole.Rules)
		return nil
	})
	return err
}

const expectedPrometheusK8RoleJSON = `
{
	"apiVersion":"rbac.authorization.k8s.io/v1",
	"kind":"Role",
	"metadata":{"name":"ocs-metrics-svc"},
	"rules":[
		{
			"apiGroups":[""],
			"resources":["services","endpoints","pods"],
			"verbs":["get","list","watch"]
		},
		{
			"apiGroups":[""],
			"resources":["persistentvolumeclaims","pods","configmaps","secrets"],
			"verbs":["get","list","watch"]
		}
	]
}
`

const expectedMetricExporterRoleJSON = `
{
	"apiVersion":"rbac.authorization.k8s.io/v1",
	"kind":"Role",
	"metadata":{"name":"ocs-metrics-exporter"},
	"rules":[
		{
			"apiGroups":[""],
			"resources":["secrets","configmaps"],
			"verbs":["get","list","watch"]
		},
		{
			"apiGroups":["monitoring.coreos.com"],
			"resources":["prometheusrules","servicemonitors"],
			"verbs":["get","list","watch","create","update","delete"]
		},
		{
			"apiGroups":["ceph.rook.io"],
			"resources":["cephobjectstores","cephclusters","cephblockpools","cephrbdmirrors"],
			"verbs":["get","list","watch"]
		},
		{
			"apiGroups":["objectbucket.io"],
			"resources":["objectbucketclaims"],
			"verbs":["get","list"]
		},
		{
			"apiGroups":["ocs.openshift.io"],
			"resources":["storageconsumers","storageclusters"],
			"verbs":["get","list","watch"]
		}
	]
}
`

func createMetricsExporterRoles(ctx context.Context,
	r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {
	// create/update prometheus server needed roles
	var expectedRole = new(rbacv1.Role)
	err := json.Unmarshal([]byte(expectedPrometheusK8RoleJSON), expectedRole)
	if err != nil {
		r.Log.Error(err, "an unexpected error occurred while unmarshalling prometheus role")
		return err
	}
	currentRole := new(rbacv1.Role)
	expectedRole.Name, currentRole.Name = metricsExporterRoleName, metricsExporterRoleName
	expectedRole.Namespace, currentRole.Namespace = instance.Namespace, instance.Namespace
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, currentRole, func() error {
		expectedRulesLen := len(expectedRole.Rules)
		if len(currentRole.Rules) != expectedRulesLen {
			currentRole.Rules = make([]rbacv1.PolicyRule, expectedRulesLen)
		}
		copy(currentRole.Rules, expectedRole.Rules)
		currentRole.OwnerReferences = []metav1.OwnerReference{{
			APIVersion: instance.APIVersion,
			Kind:       instance.Kind,
			Name:       instance.Name,
			UID:        instance.UID,
		}}
		return nil
	})
	if err != nil {
		r.Log.Error(err, "failed to create/update prometheus roles")
		return err
	}

	// create/update metrics exporter roles
	expectedRole = new(rbacv1.Role)
	err = json.Unmarshal([]byte(expectedMetricExporterRoleJSON), expectedRole)
	if err != nil {
		r.Log.Error(err, "an unexpected error occurred while unmarshalling metrics exporter role")
		return err
	}
	currentRole = new(rbacv1.Role)
	expectedRole.Name, currentRole.Name = metricsExporterName, metricsExporterName
	expectedRole.Namespace, currentRole.Namespace = instance.Namespace, instance.Namespace
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, currentRole, func() error {
		expectedRulesLen := len(expectedRole.Rules)
		if len(currentRole.Rules) != expectedRulesLen {
			currentRole.Rules = make([]rbacv1.PolicyRule, expectedRulesLen)
		}
		copy(currentRole.Rules, expectedRole.Rules)
		currentRole.OwnerReferences = []metav1.OwnerReference{{
			APIVersion: instance.APIVersion,
			Kind:       instance.Kind,
			Name:       instance.Name,
			UID:        instance.UID,
		}}
		return nil
	})

	return err
}

const expectedPrometheusK8RoleBindingJSON = `
{
	"apiVersion":"rbac.authorization.k8s.io/v1",
	"kind":"RoleBinding",
	"metadata":{"name":"ocs-metrics-svc"},
	"roleRef":{
		"apiGroup":"rbac.authorization.k8s.io",
		"kind":"Role",
		"name":"ocs-metrics-svc"
	},
	"subjects":[
		{
			"kind":"ServiceAccount",
			"name":"prometheus-k8s",
			"namespace":"openshift-monitoring"
		}
	]
}`

// expectedMetricsExporterRoleBindingJSON rolebindings for metrics exporter
// it doesn't contain 'subject' part, which is added in the create code below
const expectedMetricsExporterRoleBindingJSON = `
{
	"apiVersion":"rbac.authorization.k8s.io/v1",
	"kind":"RoleBinding",
	"metadata":{"name":"ocs-metrics-exporter"},
	"roleRef":{
		"apiGroup":"rbac.authorization.k8s.io",
		"kind":"Role",
		"name":"ocs-metrics-exporter"
	}
}`

func createMetricsExporterRolebindings(ctx context.Context,
	r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {
	// rolebinding for prometheus
	var expectedRoleBinding = new(rbacv1.RoleBinding)
	err := json.Unmarshal([]byte(expectedPrometheusK8RoleBindingJSON), expectedRoleBinding)
	if err != nil {
		r.Log.Error(err,
			"an unexpected error occurred while unmarshalling prometheus rolebinding")
		return err
	}
	currentRoleBinding := new(rbacv1.RoleBinding)
	expectedRoleBinding.Name, currentRoleBinding.Name = metricsExporterRoleName, metricsExporterRoleName
	expectedRoleBinding.Namespace, currentRoleBinding.Namespace = instance.Namespace, instance.Namespace
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, currentRoleBinding, func() error {
		currentRoleBinding.RoleRef.APIGroup = expectedRoleBinding.RoleRef.APIGroup
		currentRoleBinding.RoleRef.Kind = expectedRoleBinding.RoleRef.Kind
		currentRoleBinding.RoleRef.Name = expectedRoleBinding.RoleRef.Name
		expectedSubjectsLen := len(expectedRoleBinding.Subjects)
		if len(currentRoleBinding.Subjects) != expectedSubjectsLen {
			currentRoleBinding.Subjects = make([]rbacv1.Subject, expectedSubjectsLen)
		}
		copy(currentRoleBinding.Subjects, expectedRoleBinding.Subjects)
		currentRoleBinding.OwnerReferences = []metav1.OwnerReference{{
			APIVersion: instance.APIVersion,
			Kind:       instance.Kind,
			Name:       instance.Name,
			UID:        instance.UID,
		}}
		return nil
	})
	if err != nil {
		r.Log.Error(err, "error while create/update prometheus rolebindings")
		return err
	}

	// rolebinding for metrics exporter
	expectedRoleBinding = new(rbacv1.RoleBinding)
	err = json.Unmarshal([]byte(expectedMetricsExporterRoleBindingJSON), expectedRoleBinding)
	if err != nil {
		r.Log.Error(err,
			"an unexpected error occurred while unmarshalling metrics exporter rolebinding")
		return err
	}
	currentRoleBinding = new(rbacv1.RoleBinding)
	expectedRoleBinding.Name, currentRoleBinding.Name = metricsExporterName, metricsExporterName
	expectedRoleBinding.Namespace, currentRoleBinding.Namespace = instance.Namespace, instance.Namespace

	expectedRoleBinding.Subjects = make([]rbacv1.Subject, 1)
	expectedRoleBinding.Subjects[0].Kind = "ServiceAccount"
	expectedRoleBinding.Subjects[0].Name = metricsExporterName
	expectedRoleBinding.Subjects[0].Namespace = instance.Namespace

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, currentRoleBinding, func() error {
		currentRoleBinding.RoleRef.APIGroup = expectedRoleBinding.RoleRef.APIGroup
		currentRoleBinding.RoleRef.Kind = expectedRoleBinding.RoleRef.Kind
		currentRoleBinding.RoleRef.Name = expectedRoleBinding.RoleRef.Name
		expectedSubjectsLen := len(expectedRoleBinding.Subjects)
		if len(currentRoleBinding.Subjects) != expectedSubjectsLen {
			currentRoleBinding.Subjects = make([]rbacv1.Subject, expectedSubjectsLen)
		}
		copy(currentRoleBinding.Subjects, expectedRoleBinding.Subjects)
		currentRoleBinding.OwnerReferences = []metav1.OwnerReference{{
			APIVersion: instance.APIVersion,
			Kind:       instance.Kind,
			Name:       instance.Name,
			UID:        instance.UID,
		}}
		return nil
	})

	return err
}
