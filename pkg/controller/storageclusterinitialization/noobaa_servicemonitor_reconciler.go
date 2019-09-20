package storageclusterinitialization

import (
	"context"
	"reflect"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/go-logr/logr"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *ReconcileStorageClusterInitialization) ensureNoobaaServiceMonitor(initialData *ocsv1.StorageClusterInitialization, reqLogger logr.Logger) error {
	sm := r.newNoobaaServiceMonitor(initialData, reqLogger)
	err := controllerutil.SetControllerReference(initialData, sm, r.scheme)
	if err != nil {
		return err
	}

	found := &monitoringv1.ServiceMonitor{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: sm.ObjectMeta.Name, Namespace: initialData.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating NooBaa ServiceMonitor")
		err = r.client.Create(context.TODO(), sm)

	} else if err == nil {
		if !reflect.DeepEqual(sm.Spec, found.Spec) {
			reqLogger.Info("Updating NooBaa ServiceMonitor")
			found.Spec = sm.Spec
			err = r.client.Update(context.TODO(), found)
		}
	}

	return nil
}

func (r *ReconcileStorageClusterInitialization) newNoobaaServiceMonitor(initialData *ocsv1.StorageClusterInitialization, reqLogger logr.Logger) *monitoringv1.ServiceMonitor {
	sm := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "noobaa-mgr",
			Namespace: initialData.Namespace,
			Labels: map[string]string{
				"app": "noobaa",
			},
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{initialData.Namespace},
			},
			Endpoints: []monitoringv1.Endpoint{
				{
					Interval: "30s",
					Port:     "mgmt",
					Path:     "/metrics",
				},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "noobaa",
				},
			},
		},
	}

	return sm
}
