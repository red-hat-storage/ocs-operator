package storagecluster

import (
	"context"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/platform"

	consolev1 "github.com/openshift/api/console/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// Name of the ConsolePlugin CR
	ODF_CONSOLE = "odf-console"
	// Alias name for the internal RGW proxy configuration (ConsolePlugin's spec)
	INTERNAL_RGW_PROXY_ALIAS = "internalRgwS3"
)

type ocsConsoleConfiguration struct{}

// ensureCreated ensures that the ConsolePlugin CR is configured with the internal RGW proxy
func (obj *ocsConsoleConfiguration) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	// Only configure for internal mode
	if instance.Spec.ExternalStorage.Enable {
		return reconcile.Result{}, nil
	}

	reconcileStrategy := ReconcileStrategy(instance.Spec.ManagedResources.CephObjectStores.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return reconcile.Result{}, nil
	}

	skip, err := platform.PlatformsShouldSkipObjectStore()
	if err != nil {
		return reconcile.Result{}, err
	}
	if skip {
		return reconcile.Result{}, nil
	}

	consolePlugin := &consolev1.ConsolePlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name: ODF_CONSOLE,
		},
	}

	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: ODF_CONSOLE}, consolePlugin)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("ConsolePlugin not found, skipping configuration", "ConsolePlugin", ODF_CONSOLE)
			return reconcile.Result{}, nil
		}
		r.Log.Error(err, "Failed to get ConsolePlugin", "ConsolePlugin", klog.KRef("", ODF_CONSOLE))
		return reconcile.Result{}, fmt.Errorf("failed to get ConsolePlugin %s: %v", ODF_CONSOLE, err)
	}

	proxyExists := false
	if consolePlugin.Spec.Proxy != nil {
		for _, proxy := range consolePlugin.Spec.Proxy {
			if proxy.Alias == INTERNAL_RGW_PROXY_ALIAS {
				proxyExists = true
				break
			}
		}
	}

	// If proxy already exists, no need to update
	if proxyExists {
		r.Log.V(4).Info("Internal RGW proxy already configured in ConsolePlugin", "ConsolePlugin", klog.KRef("", ODF_CONSOLE))
		return reconcile.Result{}, nil
	}

	serviceName := generateNameForCephObjectStoreService(instance)

	newProxy := consolev1.ConsolePluginProxy{
		Alias:         INTERNAL_RGW_PROXY_ALIAS,
		Authorization: consolev1.None,
		Endpoint: consolev1.ConsolePluginProxyEndpoint{
			Type: consolev1.ProxyTypeService,
			Service: &consolev1.ConsolePluginProxyServiceConfig{
				Name:      serviceName,
				Namespace: instance.Namespace,
				Port:      443,
			},
		},
	}

	if consolePlugin.Spec.Proxy == nil {
		consolePlugin.Spec.Proxy = []consolev1.ConsolePluginProxy{}
	}
	consolePlugin.Spec.Proxy = append(consolePlugin.Spec.Proxy, newProxy)

	err = r.Client.Update(context.TODO(), consolePlugin)
	if err != nil {
		r.Log.Error(err, "Failed to update ConsolePlugin with internal RGW proxy", "ConsolePlugin", klog.KRef("", ODF_CONSOLE))
		return reconcile.Result{}, fmt.Errorf("failed to update ConsolePlugin %s: %v", ODF_CONSOLE, err)
	}

	r.Log.Info("Successfully added internal RGW proxy configuration to ConsolePlugin", "ConsolePlugin", klog.KRef("", ODF_CONSOLE), "Service", klog.KRef(instance.Namespace, serviceName))
	return reconcile.Result{}, nil
}

// ensureDeleted removes the internal RGW proxy configuration from ConsolePlugin
func (obj *ocsConsoleConfiguration) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	// Only configured for internal mode (during ensureCreated)
	if sc.Spec.ExternalStorage.Enable {
		return reconcile.Result{}, nil
	}

	reconcileStrategy := ReconcileStrategy(sc.Spec.ManagedResources.CephObjectStores.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return reconcile.Result{}, nil
	}

	skip, err := platform.PlatformsShouldSkipObjectStore()
	if err != nil {
		return reconcile.Result{}, err
	}
	if skip {
		return reconcile.Result{}, nil
	}

	consolePlugin := &consolev1.ConsolePlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name: ODF_CONSOLE,
		},
	}

	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: ODF_CONSOLE}, consolePlugin)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Uninstall: ConsolePlugin not found, nothing to clean up", "ConsolePlugin", klog.KRef("", ODF_CONSOLE))
			return reconcile.Result{}, nil
		}
		r.Log.Error(err, "Uninstall: Failed to get ConsolePlugin", "ConsolePlugin", klog.KRef("", ODF_CONSOLE))
		return reconcile.Result{}, fmt.Errorf("uninstall: failed to get ConsolePlugin %s: %v", ODF_CONSOLE, err)
	}

	// If proxy entry not found, nothing to do
	if len(consolePlugin.Spec.Proxy) == 0 {
		r.Log.Info("Uninstall: No proxy entries found in ConsolePlugin", "ConsolePlugin", klog.KRef("", ODF_CONSOLE))
		return reconcile.Result{}, nil
	}

	proxyIndex := -1
	for i, proxy := range consolePlugin.Spec.Proxy {
		if proxy.Alias == INTERNAL_RGW_PROXY_ALIAS {
			proxyIndex = i
			break
		}
	}

	// If proxy entry not found, nothing to do
	if proxyIndex == -1 {
		r.Log.Info("Uninstall: Internal RGW proxy not found in ConsolePlugin", "ConsolePlugin", klog.KRef("", ODF_CONSOLE))
		return reconcile.Result{}, nil
	}

	// Remove the proxy entry while preserving others
	consolePlugin.Spec.Proxy = append(
		consolePlugin.Spec.Proxy[:proxyIndex],
		consolePlugin.Spec.Proxy[proxyIndex+1:]...,
	)

	err = r.Client.Update(context.TODO(), consolePlugin)
	if err != nil {
		r.Log.Error(err, "Uninstall: Failed to remove internal RGW proxy from ConsolePlugin", "ConsolePlugin", klog.KRef("", ODF_CONSOLE))
		return reconcile.Result{}, fmt.Errorf("uninstall: failed to update ConsolePlugin %s: %v", ODF_CONSOLE, err)
	}

	r.Log.Info("Uninstall: Successfully removed internal RGW proxy from ConsolePlugin", "ConsolePlugin", klog.KRef("", ODF_CONSOLE))
	return reconcile.Result{}, nil
}
