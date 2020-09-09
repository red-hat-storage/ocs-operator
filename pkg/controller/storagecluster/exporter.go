package storagecluster

import (
	"context"
	"fmt"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	exporterName   = "ocs-metrics-exporter"
	portMetrics    = "metrics"
	portExporter   = "exporter"
	metricsPath    = "/metrics"
	scrapeInterval = "1m"
)

var exporterLabels = map[string]string{
	"app.kubernetes.io/component": exporterName,
	"app.kubernetes.io/name":      exporterName,
}

// enableMetricsExporter is a wrapper around CreateOrUpdateService()
// and CreateOrUpdateServiceMonitor()
func (r *ReconcileStorageCluster) enableMetricsExporter(instance *ocsv1.StorageCluster) error {
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
func CreateOrUpdateService(r *ReconcileStorageCluster, instance *ocsv1.StorageCluster) (*corev1.Service, error) {
	service := getMetricsExporterService(instance)
	namespacedName := types.NamespacedName{Namespace: service.GetNamespace(), Name: service.GetName()}

	r.reqLogger.Info("Reconciling metrics exporter service", "NamespacedName", namespacedName)

	oldService := &corev1.Service{}
	err := r.client.Get(context.TODO(), namespacedName, oldService)
	if err != nil {
		if apierrors.IsNotFound(err) {
			err = r.client.Create(context.TODO(), service)
			if err != nil {
				return nil, fmt.Errorf("failed to create metrics exporter service %v. %v", namespacedName, err)
			}
			return service, nil
		}
		return nil, fmt.Errorf("failed to retrieve metrics exporter service %v. %v", namespacedName, err)
	}
	service.ResourceVersion = oldService.ResourceVersion
	err = r.client.Update(context.TODO(), service)
	if err != nil {
		return nil, fmt.Errorf("failed to update service %v. %v", namespacedName, err)
	}
	return service, nil
}

func getMetricsExporterServiceMonitor(instance *ocsv1.StorageCluster) *monitoringv1.ServiceMonitor {
	serviceMonitor := &monitoringv1.ServiceMonitor{
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
		Spec: monitoringv1.ServiceMonitorSpec{
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{instance.Namespace},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: exporterLabels,
			},
			Endpoints: []monitoringv1.Endpoint{
				{
					Port:     portMetrics,
					Path:     metricsPath,
					Interval: scrapeInterval,
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
func CreateOrUpdateServiceMonitor(r *ReconcileStorageCluster, instance *ocsv1.StorageCluster) (*monitoringv1.ServiceMonitor, error) {
	serviceMonitor := getMetricsExporterServiceMonitor(instance)
	namespacedName := types.NamespacedName{Name: serviceMonitor.Name, Namespace: serviceMonitor.Namespace}

	r.reqLogger.Info("Reconciling metrics exporter service monitor", "NamespacedName", namespacedName)

	oldSm := &monitoringv1.ServiceMonitor{}
	err := r.client.Get(context.TODO(), namespacedName, oldSm)
	if err != nil {
		if apierrors.IsNotFound(err) {
			err = r.client.Create(context.TODO(), serviceMonitor)
			if err != nil {
				return nil, fmt.Errorf("failed to create metrics exporter servicemonitor %v. %v", namespacedName, err)
			}
			return serviceMonitor, nil
		}
		return nil, fmt.Errorf("failed to retrieve metrics exporter servicemonitor %v. %v", namespacedName, err)
	}
	serviceMonitor.ResourceVersion = oldSm.ResourceVersion
	err = r.client.Update(context.TODO(), serviceMonitor)
	if err != nil {
		return nil, fmt.Errorf("failed to update metrics exporter servicemonitor %v. %v", namespacedName, err)
	}
	return serviceMonitor, nil
}
