package storagecluster

import (
	"context"

	"github.com/ghodss/yaml"
	consolev1 "github.com/openshift/api/console/v1"
	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

type ocsQuickStarts struct{}

func (obj *ocsQuickStarts) ensureCreated(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) error {
	qscrd := extv1.CustomResourceDefinition{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: "consolequickstarts.console.openshift.io", Namespace: ""}, &qscrd)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.V(2).Info("No custom resource definition found for consolequickstart. Skipping quickstart initialization.")
			return nil
		}
		return err
	}
	if len(AllQuickStarts) == 0 {
		r.Log.Info("No quickstarts found.")
		return nil
	}
	for _, qs := range AllQuickStarts {
		cqs := consolev1.ConsoleQuickStart{}
		err := yaml.Unmarshal(qs, &cqs)
		if err != nil {
			r.Log.Error(err, "Failed to unmarshal ConsoleQuickStart.", "ConsoleQuickStartString", string(qs))
			continue
		}
		found := consolev1.ConsoleQuickStart{}
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: cqs.Name, Namespace: cqs.Namespace}, &found)
		if err != nil {
			if errors.IsNotFound(err) {
				err = r.Client.Create(context.TODO(), &cqs)
				if err != nil {
					r.Log.Error(err, "Failed to create QuickStart.", "QuickStart", klog.KRef(cqs.Namespace, cqs.Name))
					return nil
				}
				r.Log.Info("Creating Quickstarts.", "QuickStart", klog.KRef(cqs.Namespace, cqs.Name))
				continue
			}
			r.Log.Error(err, "Error has occurred when fetching QuickStarts.", "QuickStart", klog.KRef(cqs.Namespace, cqs.Name))
			return nil
		}
		found.Spec = cqs.Spec
		err = r.Client.Update(context.TODO(), &found)
		if err != nil {
			r.Log.Error(err, "Failed to update QuickStart.", "QuickStart", klog.KRef(cqs.Namespace, cqs.Name))
			return nil
		}
		r.Log.Info("Updating QuickStarts.", "QuickStart", klog.KRef(cqs.Namespace, cqs.Name))
	}
	return nil
}

func (obj *ocsQuickStarts) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) error {
	if len(AllQuickStarts) == 0 {
		r.Log.Info("No quickstarts found.")
		return nil
	}
	for _, qs := range AllQuickStarts {
		cqs := consolev1.ConsoleQuickStart{}
		err := yaml.Unmarshal(qs, &cqs)
		if err != nil {
			r.Log.Error(err, "Failed to unmarshal ConsoleQuickStart.", "ConsoleQuickStartString", string(qs))
			continue
		}
		found := consolev1.ConsoleQuickStart{}
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: cqs.Name, Namespace: cqs.Namespace}, &found)
		if err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			r.Log.Error(err, "Uninstall: Failed to get QuickStart.", "QuickStart", klog.KRef(cqs.Namespace, cqs.Name))
			return nil
		}
		err = r.Client.Delete(context.TODO(), &found)
		if err != nil {
			r.Log.Error(err, "Uninstall: Failed to delete QuickStart.", "QuickStart", klog.KRef(cqs.Namespace, cqs.Name))
			return nil
		}
		r.Log.Info("Uninstall: Deleting QuickStart.", "QuickStart", klog.KRef(cqs.Namespace, cqs.Name))
	}
	return nil
}
