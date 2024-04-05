package storagecluster

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"time"

	"go.uber.org/multierr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocsv1 "github.com/red-hat-storage/ocs-operator/v4/api/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/services/provider/server"
)

const (
	ocsProviderServerName               = "ocs-provider-server"
	providerAPIServerImage              = "PROVIDER_API_SERVER_IMAGE"
	onboardingSecretGeneratorImage      = "ONBOARDING_SECRET_GENERATOR_IMAGE"
	onboardingJobName                   = "onboarding-secret-generator"
	onboardingTicketPublicKeySecretName = "onboarding-ticket-key"

	ocsProviderServicePort     = int32(50051)
	ocsProviderServiceNodePort = int32(31659)

	ocsProviderCertSecretName = ocsProviderServerName + "-cert"
)

type ocsProviderServer struct{}

func (o *ocsProviderServer) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {

	if !instance.Spec.AllowRemoteStorageConsumers {
		r.Log.Info("Spec.AllowRemoteStorageConsumers is disabled")
		if err := r.verifyNoStorageConsumerExist(instance); err != nil {
			return reconcile.Result{}, err
		}
		return o.ensureDeleted(r, instance)
	}

	r.Log.Info("Spec.AllowRemoteStorageConsumers is enabled. Creating Provider API resources")

	if err := o.createSecret(r, instance); err != nil {
		return reconcile.Result{}, err
	}

	if res, err := o.createService(r, instance); err != nil {
		return reconcile.Result{}, err
	} else if !res.IsZero() {
		return res, nil
	}

	if res, err := o.createDeployment(r, instance); err != nil {
		return reconcile.Result{}, err
	} else if !res.IsZero() {
		return res, nil
	}

	if res, err := o.createJob(r, instance); err != nil {
		return reconcile.Result{}, err
	} else if !res.IsZero() {
		return res, nil
	}

	return reconcile.Result{}, nil
}

func (o *ocsProviderServer) ensureDeleted(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {

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
		instance.Status.StorageProviderEndpoint = ""
	}

	return reconcile.Result{}, finalErr
}

func (o *ocsProviderServer) createDeployment(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {

	var finalErr error

	for _, env := range []string{providerAPIServerImage, util.WatchNamespaceEnvVar} {
		if _, ok := os.LookupEnv(env); !ok {
			multierr.AppendInto(&finalErr, fmt.Errorf("ENV var %s not found", env))
		}
	}

	if finalErr != nil {
		return reconcile.Result{}, finalErr
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
		return reconcile.Result{}, err
	}

	err = o.ensureDeploymentReplica(actualDeployment, desiredDeployment)
	if err != nil {
		r.recorder.ReportIfNotPresent(instance, corev1.EventTypeNormal, "Waiting", "Waiting for Deployment to become ready "+desiredDeployment.Name)
		r.Log.Info("Waiting for Deployment to become ready", "Name", desiredDeployment.Name)
		return reconcile.Result{RequeueAfter: time.Second * 5}, nil
	}

	r.Log.Info("Deployment is running as desired")
	return reconcile.Result{}, nil
}

func (o *ocsProviderServer) createService(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {

	if instance.Spec.ProviderAPIServerServiceType != "" {
		switch instance.Spec.ProviderAPIServerServiceType {
		case corev1.ServiceTypeClusterIP, corev1.ServiceTypeLoadBalancer, corev1.ServiceTypeNodePort:
		default:
			err := fmt.Errorf("providerAPIServer only supports service of type %s, %s and %s",
				corev1.ServiceTypeNodePort, corev1.ServiceTypeLoadBalancer, corev1.ServiceTypeClusterIP)
			r.Log.Error(err, "Failed to create/update service, Requested ServiceType is", "ServiceType", instance.Spec.ProviderAPIServerServiceType)
			return reconcile.Result{}, err
		}

	}

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
			desiredService.Spec.ClusterIP = actualService.Spec.ClusterIP
			desiredService.Spec.IPFamilies = actualService.Spec.IPFamilies

			if actualService.Annotations == nil {
				actualService.Annotations = map[string]string{}
			}

			for key, value := range desiredService.Annotations {
				actualService.Annotations[key] = value
			}

			actualService.Spec = desiredService.Spec
			return controllerutil.SetOwnerReference(instance, actualService, r.Client.Scheme())
		},
	)
	if err != nil {
		r.Log.Error(err, "Failed to create/update service", "Name", desiredService.Name)
		return reconcile.Result{}, err
	}

	r.Log.Info("Service create/update succeeded")

	switch instance.Spec.ProviderAPIServerServiceType {
	case corev1.ServiceTypeLoadBalancer:
		endpoint := o.getLoadBalancerServiceEndpoint(actualService)

		if endpoint == "" {
			r.recorder.ReportIfNotPresent(instance, corev1.EventTypeNormal, "Waiting", "Waiting for Ingress on service "+actualService.Name)
			r.Log.Info("Waiting for Ingress on service", "Service", actualService.Name, "Status", actualService.Status)
			return reconcile.Result{RequeueAfter: time.Second * 5}, nil
		}

		instance.Status.StorageProviderEndpoint = fmt.Sprintf("%s:%d", endpoint, ocsProviderServicePort)

	case corev1.ServiceTypeClusterIP:
		instance.Status.StorageProviderEndpoint = fmt.Sprintf("%s:%d", actualService.Spec.ClusterIP, ocsProviderServicePort)

	default: // Nodeport is the default ServiceType for the provider server
		nodeAddresses, err := o.getWorkerNodesInternalIPAddresses(r)
		if err != nil {
			return reconcile.Result{}, err
		}

		if len(nodeAddresses) == 0 {
			err = fmt.Errorf("Could not find any worker nodes")
			r.Log.Error(err, "Worker nodes count is zero")
			return reconcile.Result{}, err
		}

		instance.Status.StorageProviderEndpoint = fmt.Sprintf("%s:%d", nodeAddresses[0], ocsProviderServiceNodePort)

	}

	r.Log.Info("status.storageProviderEndpoint is updated", "Endpoint", instance.Status.StorageProviderEndpoint)

	return reconcile.Result{}, nil
}

func (o *ocsProviderServer) getLoadBalancerServiceEndpoint(service *corev1.Service) string {
	endpoint := ""

	if len(service.Status.LoadBalancer.Ingress) != 0 {
		if service.Status.LoadBalancer.Ingress[0].IP != "" {
			endpoint = service.Status.LoadBalancer.Ingress[0].IP
		} else if service.Status.LoadBalancer.Ingress[0].Hostname != "" {
			endpoint = service.Status.LoadBalancer.Ingress[0].Hostname
		}
	}

	return endpoint
}

// getWorkerNodesInternalIPAddresses return slice of Internal IPAddress of worker nodes
func (o *ocsProviderServer) getWorkerNodesInternalIPAddresses(r *StorageClusterReconciler) ([]string, error) {

	nodes := &corev1.NodeList{}

	err := r.Client.List(context.TODO(), nodes)
	if err != nil {
		r.Log.Error(err, "Failed to list nodes")
		return nil, err
	}

	nodeAddresses := []string{}

	for i := range nodes.Items {
		node := &nodes.Items[i]
		if _, ok := node.ObjectMeta.Labels["node-role.kubernetes.io/worker"]; ok {
			for _, address := range node.Status.Addresses {
				if address.Type == corev1.NodeInternalIP {
					nodeAddresses = append(nodeAddresses, address.Address)
					break
				}
			}
		}
	}

	sort.Strings(nodeAddresses)

	return nodeAddresses, nil
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
									ContainerPort: ocsProviderServicePort,
								},
							},
							SecurityContext: &corev1.SecurityContext{
								RunAsNonRoot:           ptr.To(true),
								ReadOnlyRootFilesystem: ptr.To(true),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "cert-secret",
									MountPath: server.ProviderCertsMountPoint,
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "cert-secret",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: ocsProviderCertSecretName,
								},
							},
						},
					},
					ServiceAccountName: ocsProviderServerName,
				},
			},
		},
	}
}

func GetProviderAPIServerService(instance *ocsv1.StorageCluster) *corev1.Service {

	if instance.Spec.ProviderAPIServerServiceType == "" {
		instance.Spec.ProviderAPIServerServiceType = corev1.ServiceTypeNodePort
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ocsProviderServerName,
			Namespace: instance.Namespace,
			Annotations: map[string]string{
				"service.beta.openshift.io/serving-cert-secret-name": ocsProviderCertSecretName,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": "ocsProviderApiServer",
			},
			Ports: []corev1.ServicePort{
				{
					NodePort: func() int32 {
						// ClusterIP service doesn't need nodePort
						if instance.Spec.ProviderAPIServerServiceType == corev1.ServiceTypeClusterIP {
							return 0
						}
						return ocsProviderServiceNodePort
					}(),
					Port:       ocsProviderServicePort,
					TargetPort: intstr.FromString("ocs-provider"),
				},
			},
			Type: instance.Spec.ProviderAPIServerServiceType,
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

// RandomString - Generate a random string of A-Z chars with len = l
func RandomString(l int) string {

	bytes := make([]byte, l)
	for i := 0; i < l; i++ {
		bytes[i] = byte(65 + rand.Intn(25)) //A=65 and Z = 65+25
	}

	return string(bytes)
}

func getOnboardingJobObject(instance *ocsv1.StorageCluster) *batchv1.Job {

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      onboardingJobName,
			Namespace: instance.Namespace,
		},
		Spec: batchv1.JobSpec{
			// Eligible to delete automatically when job finishes
			TTLSecondsAfterFinished: ptr.To(int32(0)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyOnFailure,
					ServiceAccountName: onboardingJobName,
					Containers: []corev1.Container{
						{
							Name:    onboardingJobName,
							Image:   os.Getenv(onboardingSecretGeneratorImage),
							Command: []string{"/usr/local/bin/onboarding-secret-generator"},
							Env: []corev1.EnvVar{
								{
									Name:  util.OperatorNamespaceEnvVar,
									Value: os.Getenv(util.OperatorNamespaceEnvVar),
								},
							},
						},
					},
				},
			},
		},
	}
}

func (o *ocsProviderServer) createJob(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	var err error
	if os.Getenv(onboardingSecretGeneratorImage) == "" {
		err = fmt.Errorf("OnboardingSecretGeneratorImage env var is not set")
		r.Log.Error(err, "No value set for env variable")

		return reconcile.Result{}, err
	}

	actualSecret := &corev1.Secret{}
	// Creating the job only if public is not found
	err = r.Client.Get(context.Background(), types.NamespacedName{Name: onboardingTicketPublicKeySecretName,
		Namespace: instance.Namespace}, actualSecret)

	if errors.IsNotFound(err) {
		onboardingSecretGeneratorJob := getOnboardingJobObject(instance)
		err = r.Client.Create(context.Background(), onboardingSecretGeneratorJob)
	}
	if err != nil {
		r.Log.Error(err, "failed to create/ensure secret")
		return reconcile.Result{}, err
	}

	r.Log.Info("Job is running as desired")
	return reconcile.Result{}, nil
}
