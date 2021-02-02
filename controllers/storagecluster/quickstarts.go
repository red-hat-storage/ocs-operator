package storagecluster

import (
	"context"

	"github.com/ghodss/yaml"
	consolev1 "github.com/openshift/api/console/v1"
	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

type ocsQuickStarts struct{}

func (obj *ocsQuickStarts) ensureCreated(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) error {
	qscrd := extv1.CustomResourceDefinition{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: "consolequickstarts.console.openshift.io", Namespace: ""}, &qscrd)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.V(2).Info("No custom resource definition found for consolequickstart. Skipping quickstart initialization")
			return nil
		}
		return err
	}
	if len(AllQuickStarts) == 0 {
		r.Log.Info("No quickstarts found")
		return nil
	}
	for _, qs := range AllQuickStarts {
		cqs := consolev1.ConsoleQuickStart{}
		err := yaml.Unmarshal(qs, &cqs)
		if err != nil {
			r.Log.Error(err, "Failed to unmarshal ConsoleQuickStart", "ConsoleQuickStartString", string(qs))
			continue
		}
		found := consolev1.ConsoleQuickStart{}
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: cqs.Name, Namespace: cqs.Namespace}, &found)
		if err != nil {
			if errors.IsNotFound(err) {
				err = r.Client.Create(context.TODO(), &cqs)
				if err != nil {
					r.Log.Error(err, "Failed to create quickstart", "Name", cqs.Name, "Namespace", cqs.Namespace)
					return nil
				}
				r.Log.Info("Creating quickstarts", "Name", cqs.Name, "Namespace", cqs.Namespace)
				continue
			}
			r.Log.Error(err, "Error has occurred when fetching quickstarts")
			return nil
		}
		found.Spec = cqs.Spec
		err = r.Client.Update(context.TODO(), &found)
		if err != nil {
			r.Log.Error(err, "Failed to update quickstart", "Name", cqs.Name, "Namespace", cqs.Namespace)
			return nil
		}
		r.Log.Info("Updating quickstarts", "Name", cqs.Name, "Namespace", cqs.Namespace)
	}
	return nil
}

func (obj *ocsQuickStarts) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) error {
	if len(AllQuickStarts) == 0 {
		r.Log.Info("No quickstarts found")
		return nil
	}
	for _, qs := range AllQuickStarts {
		cqs := consolev1.ConsoleQuickStart{}
		err := yaml.Unmarshal(qs, &cqs)
		if err != nil {
			r.Log.Error(err, "Failed to unmarshal ConsoleQuickStart", "ConsoleQuickStartString", string(qs))
			continue
		}
		found := consolev1.ConsoleQuickStart{}
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: cqs.Name, Namespace: cqs.Namespace}, &found)
		if err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			r.Log.Error(err, "Uninstall: Failed to get QuickStart %s", "Name", cqs.Name, "Namespace", cqs.Namespace)
			return nil
		}
		err = r.Client.Delete(context.TODO(), &found)
		if err != nil {
			r.Log.Error(err, "Uninstall: Failed to delete QuickStart %s", "Name", cqs.Name, "Namespace", cqs.Namespace)
			return nil
		}
		r.Log.Info("Uninstall: Deleting QuickStart", "Name", cqs.Name, "Namespace", cqs.Namespace)
	}
	return nil
}
