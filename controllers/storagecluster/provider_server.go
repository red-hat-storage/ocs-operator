package storagecluster

import (
	"context"
	"fmt"
	"math/rand"
	"os"

	"go.uber.org/multierr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	"github.com/red-hat-storage/ocs-operator/controllers/util"
)

const (
	ocsProviderServerName  = "ocs-provider-server"
	providerAPIServerImage = "PROVIDER_API_SERVER_IMAGE"
)

type ocsProviderServer struct{}

func (o *ocsProviderServer) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {

	if !instance.Spec.AllowRemoteStorageConsumers {
		r.Log.Info("Spec.AllowRemoteStorageConsumers is disabled")
		return o.ensureDeleted(r, instance)
	}

	r.Log.Info("Spec.AllowRemoteStorageConsumers is enabled. Creating Provider API resources")

	var finalErr error

	for _, f := range []func(*StorageClusterReconciler, *ocsv1.StorageCluster) error{
		o.createSecret,
		o.createService,
		o.createDeployment,
	} {
		err := f(r, instance)
		if err != nil {
			multierr.AppendInto(&finalErr, err)
			continue
		}
	}

	return finalErr
}

func (o *ocsProviderServer) ensureDeleted(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {

	// We do not check instance.Spec.AllowRemoteStorageConsumers because provider can disable this functionality
	// and we need to delete the resources even the flag is not enabled (uninstall case).

	// This func is directly called by the ensureCreated if the flag is disabled and deletes the resource
	// Which means we do not need to call ensureDeleted while reconciling unless we are uninstalling

	// NOTE: Do not add the check

	var finalErr error

	for _, resource := range []client.Object{
		GetProviderAPIServerSecret(instance),
		GetProviderAPIServerService(instance),
		GetProviderAPIServerDeployment(instance),
	} {
		err := r.Client.Delete(context.TODO(), resource)

		if err != nil && !errors.IsNotFound(err) {
			r.Log.Error(err, "Failed to delete resource", "Kind", resource.GetObjectKind(), "Name", resource.GetName())
			multierr.AppendInto(&finalErr, err)
		}
	}

	if finalErr == nil {
		r.Log.Info("Resource deletion for provider succeeded")
	}

	return finalErr
}

func (o *ocsProviderServer) createDeployment(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {

	var finalErr error

	for _, env := range []string{providerAPIServerImage, util.WatchNamespaceEnvVar} {
		if _, ok := os.LookupEnv(env); !ok {
			multierr.AppendInto(&finalErr, fmt.Errorf("ENV var %s not found", env))
		}
	}

	if finalErr != nil {
		return finalErr
	}

	desiredDeployment := GetProviderAPIServerDeployment(instance)
	actualDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desiredDeployment.Name,
			Namespace: desiredDeployment.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(
		context.TODO(), r.Client, actualDeployment,
		func() error {
			actualDeployment.Spec = desiredDeployment.Spec
			return controllerutil.SetOwnerReference(instance, actualDeployment, r.Client.Scheme())
		},
	)
	if err != nil && !errors.IsAlreadyExists(err) {
		r.Log.Error(err, "Failed to create/update deployment", "Name", desiredDeployment.Name)
		return err
	}

	err = o.ensureDeploymentReplica(actualDeployment, desiredDeployment)
	if err != nil {
		r.Log.Error(err, "Deployment is not ready", "Name", desiredDeployment.Name)
		return err
	}

	r.Log.Info("Deployment is running as desired")
	return nil
}

func (o *ocsProviderServer) createService(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {

	desiredService := GetProviderAPIServerService(instance)
	actualService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desiredService.Name,
			Namespace: desiredService.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(
		context.TODO(), r.Client, actualService,
		func() error {
			actualService.Spec = desiredService.Spec
			return controllerutil.SetOwnerReference(instance, actualService, r.Client.Scheme())
		},
	)
	if err != nil && !errors.IsAlreadyExists(err) {
		r.Log.Error(err, "Failed to create/update service", "Name", desiredService.Name)
		return err
	}

	r.Log.Info("Service create/update succeeded")
	return nil
}

func (o *ocsProviderServer) createSecret(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {

	desiredSecret := GetProviderAPIServerSecret(instance)
	actualSecret := &corev1.Secret{}

	err := r.Client.Get(context.TODO(), client.ObjectKeyFromObject(desiredSecret), actualSecret)

	if err != nil && errors.IsNotFound(err) {
		err = r.Client.Create(context.TODO(), desiredSecret)
		if err != nil {
			r.Log.Error(err, "Failed to create secret", "Name", desiredSecret.Name)
			return err
		}
		r.Log.Info("Secret creation succeeded", "Name", desiredSecret.Name)
	} else if err != nil {
		r.Log.Error(err, "Failed to get secret", "Name", desiredSecret.Name)
		return err
	}

	return nil
}

func (o *ocsProviderServer) ensureDeploymentReplica(actual, desired *appsv1.Deployment) error {

	if actual.Status.AvailableReplicas != *desired.Spec.Replicas {
		return fmt.Errorf("Deployment %s is not ready", desired.Name)
	}

	return nil
}

func GetProviderAPIServerDeployment(instance *ocsv1.StorageCluster) *appsv1.Deployment {

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ocsProviderServerName,
			Namespace: instance.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: func(i int32) *int32 { return &i }(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "ocsProviderApiServer",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "ocsProviderApiServer",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "ocs-provider-api-server",
							Image:   os.Getenv(providerAPIServerImage),
							Command: []string{"/usr/local/bin/provider-api"},
							Env: []corev1.EnvVar{
								{
									Name:  util.WatchNamespaceEnvVar,
									Value: os.Getenv(util.WatchNamespaceEnvVar),
								},
								{
									Name:  "STORAGE_CLUSTER_NAME",
									Value: instance.Name,
								},
								{
									Name:  "STORAGE_CLUSTER_UID",
									Value: string(instance.UID),
								},
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "ocs-provider",
									ContainerPort: int32(50051),
								},
							},
						},
					},
				},
			},
		},
	}
}

func GetProviderAPIServerService(instance *ocsv1.StorageCluster) *corev1.Service {

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ocsProviderServerName,
			Namespace: instance.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": "ocsProviderApiServer",
			},
			Ports: []corev1.ServicePort{
				{
					Port:       int32(50051),
					TargetPort: intstr.FromString("ocs-provider"),
				},
			},
			Type: corev1.ServiceTypeNodePort,
		},
	}
}

func GetProviderAPIServerSecret(instance *ocsv1.StorageCluster) *corev1.Secret {

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ocsProviderServerName,
			Namespace: instance.Namespace,
		},
		Immutable: func(flag bool) *bool { return &flag }(true),
		StringData: map[string]string{
			"Key": RandomString(1024),
		},
	}
}

//RandomString - Generate a random string of A-Z chars with len = l
func RandomString(l int) string {

	bytes := make([]byte, l)
	for i := 0; i < l; i++ {
		bytes[i] = byte(65 + rand.Intn(25)) //A=65 and Z = 65+25
	}

	return string(bytes)
}
