package storagecluster

import (
	"context"
	"github.com/ghodss/yaml"
	"github.com/go-logr/logr"
	consolev1 "github.com/openshift/api/console/v1"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

func (r *ReconcileStorageCluster) ensureQuickStarts(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	if len(AllQuickStarts) == 0 {
		reqLogger.Info("No quickstarts found")
		return nil
	}
	for _, qs := range AllQuickStarts {
		cqs := consolev1.ConsoleQuickStart{}
		err := yaml.Unmarshal(qs, &cqs)
		if err != nil {
			reqLogger.Error(err, "Failed to unmarshal ConsoleQuickStart", "ConsoleQuickStartString", string(qs))
			continue
		}
		found := consolev1.ConsoleQuickStart{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: cqs.Name, Namespace: cqs.Namespace}, &found)
		if err != nil {
			if errors.IsNotFound(err) {
				err = r.client.Create(context.TODO(), &cqs)
				if err != nil {
					reqLogger.Error(err, "Failed to create quickstart", "Name", cqs.Name, "Namespace", cqs.Namespace)
					return nil
				}
				reqLogger.Info("Creating quickstarts", "Name", cqs.Name, "Namespace", cqs.Namespace)
				continue
			}
			reqLogger.Error(err, "Error has occurred when fetching quickstarts")
			return nil
		}
		found.Spec = cqs.Spec
		err = r.client.Update(context.TODO(), &found)
		if err != nil {
			reqLogger.Error(err, "Failed to update quickstart", "Name", cqs.Name, "Namespace", cqs.Namespace)
			return nil
		}
		reqLogger.Info("Updating quickstarts", "Name", cqs.Name, "Namespace", cqs.Namespace)
	}
	return nil
}
