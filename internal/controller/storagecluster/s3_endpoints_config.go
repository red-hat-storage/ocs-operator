package storagecluster

import (
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"

	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type s3EndpointConfig struct {
	EndpointURL string `json:"endpointUrl"`
}

type ocsS3EndpointsConfig struct{}

var _ resourceManager = &ocsS3EndpointsConfig{}

func (obj *ocsS3EndpointsConfig) ensureCreated(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (reconcile.Result, error) {
	s3Endpoints := map[string]s3EndpointConfig{}
	err := getHTTPSS3Endpoints(r, storageCluster.Namespace, s3Endpoints)
	if err != nil {
		return reconcile.Result{}, err
	}

	configMap := &corev1.ConfigMap{}
	configMap.Name = util.OcsHubS3EndpointsConfigMapName
	configMap.Namespace = storageCluster.Namespace

	_, err = ctrl.CreateOrUpdate(r.ctx, r.Client, configMap, func() error {
		if err := controllerutil.SetControllerReference(storageCluster, configMap, r.Scheme); err != nil {
			return err
		}
		configMap.Data = map[string]string{}
		for key, s3Endpoint := range s3Endpoints {
			configMap.Data[key] = string(util.JsonMustMarshal(s3Endpoint))
		}
		return nil
	})
	if err != nil {
		r.Log.Error(err, "failed to create or update S3 endpoints ConfigMap")
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// ensureDeleted is dummy func for the ocsS3EndpointsConfig
func (obj *ocsS3EndpointsConfig) ensureDeleted(_ *StorageClusterReconciler, _ *ocsv1.StorageCluster) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

func getHTTPSS3Endpoints(r *StorageClusterReconciler, namespace string, s3Endpoints map[string]s3EndpointConfig) error {
	// Can be extended for RGW S3, NooBaa IAM, etc.
	err := getNoobaaS3Endpoint(r, namespace, s3Endpoints)
	if err != nil {
		return err
	}

	return nil
}

func getNoobaaS3Endpoint(r *StorageClusterReconciler, namespace string, s3Endpoints map[string]s3EndpointConfig) error {
	s3Route := &routev1.Route{}
	s3Route.Name = util.NoobaaS3RouteName
	s3Route.Namespace = namespace
	if err := r.Get(r.ctx, client.ObjectKeyFromObject(s3Route), s3Route); err != nil {
		if kerrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get NooBaa S3 Route: %v", err)
	}

	if s3Route.Spec.TLS == nil {
		r.Log.Info("NooBaa S3 Route has no TLS termination, skipping non-HTTPS endpoint")
		return nil
	}

	host, found := util.GetAdmittedRouteHost(s3Route)
	if !found {
		return nil
	}

	s3Endpoints[util.NoobaaS3EndpointKey] = s3EndpointConfig{
		EndpointURL: "https://" + host,
	}

	return nil
}
