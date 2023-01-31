package storagecluster

import (
	"context"
	"fmt"

	"github.com/imdario/mergo"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	exporterName         = "ocs-metrics-exporter"
	portMetrics          = "metrics"
	portExporter         = "exporter"
	metricsPath          = "/metrics"
	rbdMirrorMetricsPath = "/metrics/rbd-mirror"
	scrapeInterval       = "1m"
)

var exporterLabels = map[string]string{
	"app.kubernetes.io/component": exporterName,
	"app.kubernetes.io/name":      exporterName,
}

// enableMetricsExporter is a wrapper around CreateOrUpdateService()
// and CreateOrUpdateServiceMonitor()
func (r *StorageClusterReconciler) enableMetricsExporter(instance *ocsv1.StorageCluster) error {
	_, err := CreateOrUpdateService(r, instance)
	if err != nil {
		return err
	}
	_, err = CreateOrUpdateServiceMonitor(r, instance)
	if err != nil {
		return err
	}
	return nil
}

func getMetricsExporterService(instance *ocsv1.StorageCluster) *corev1.Service {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      exporterName,
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

// CreateOrUpdateService creates service object or an error
func CreateOrUpdateService(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (*corev1.Service, error) {
	service := getMetricsExporterService(instance)
	namespacedName := types.NamespacedName{Namespace: service.GetNamespace(), Name: service.GetName()}

	r.Log.Info("Reconciling metrics exporter service", "NamespacedName", namespacedName)

	oldService := &corev1.Service{}
	err := r.Client.Get(context.TODO(), namespacedName, oldService)
	if err != nil {
		if apierrors.IsNotFound(err) {
			err = r.Client.Create(context.TODO(), service)
			if err != nil {
				return nil, fmt.Errorf("failed to create metrics exporter service %v. %v", namespacedName, err)
			}
			return service, nil
		}
		return nil, fmt.Errorf("failed to retrieve metrics exporter service %v. %v", namespacedName, err)
	}
	service.ResourceVersion = oldService.ResourceVersion
	service.Spec.ClusterIP = oldService.Spec.ClusterIP
	err = r.Client.Update(context.TODO(), service)
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
	relabelConfig := monitoringv1.RelabelConfig{
		TargetLabel: "managedBy",
		Replacement: instance.Name,
	}

	serviceMonitor := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      exporterName,
			Namespace: instance.Namespace,
			Labels:    serviceMonitorLabels,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: instance.APIVersion,
					Kind:       instance.Kind,
					Name:       instance.Name,
					UID:        instance.UID,
				},
			},
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
					RelabelConfigs: []*monitoringv1.RelabelConfig{&relabelConfig},
				},
				{
					Port:           portMetrics,
					Path:           rbdMirrorMetricsPath,
					Interval:       scrapeInterval,
					RelabelConfigs: []*monitoringv1.RelabelConfig{&relabelConfig},
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

// CreateOrUpdateServiceMonitor creates serviceMonitor object or an error
func CreateOrUpdateServiceMonitor(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (*monitoringv1.ServiceMonitor, error) {
	serviceMonitor := getMetricsExporterServiceMonitor(instance)
	namespacedName := types.NamespacedName{Name: serviceMonitor.Name, Namespace: serviceMonitor.Namespace}

	err := mergo.Merge(&serviceMonitor.Labels, instance.Spec.Monitoring.Labels, mergo.WithOverride)
	if err != nil {
		return nil, err
	}

	r.Log.Info("Reconciling metrics exporter service monitor", "NamespacedName", namespacedName)

	oldSm := &monitoringv1.ServiceMonitor{}
	err = r.Client.Get(context.TODO(), namespacedName, oldSm)
	if err != nil {
		if apierrors.IsNotFound(err) {
			err = r.Client.Create(context.TODO(), serviceMonitor)
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

	err = r.Client.Update(context.TODO(), oldSm)
	if err != nil {
		return nil, fmt.Errorf("failed to update metrics exporter servicemonitor %v. %v", namespacedName, err)
	}
	return serviceMonitor, nil
}
