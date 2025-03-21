package storagecluster

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
)

func TestOcsProviderServerEnsureCreated(t *testing.T) {

	t.Run("Ensure that Deployment,Service is created when storageCluster is created", func(t *testing.T) {

		r, instance := createSetupForOcsProviderTest(t, "")

		obj := &ocsProviderServer{}
		res, err := obj.ensureCreated(r, instance)
		assert.NoError(t, err)
		assert.False(t, res.IsZero())

		// storagecluster controller waits for svc status to fetch the IP and it requeue
		// as we are using a fake client and it does not fill the status automatically.
		// update the required status field of the svc to overcome the failure and requeue.
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
		}
		service.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
			{
				Hostname: "fake",
			},
		}
		err = r.Status().Update(context.TODO(), service)
		assert.NoError(t, err)

		// call ensureCreated again after filling the status of svc, It will fail on deployment now
		res, err = obj.ensureCreated(r, instance)
		assert.NoError(t, err)
		assert.False(t, res.IsZero())

		// storagecluster controller waits for deployment status to fetch the replica count and it requeue
		// as we are using a fake client and it does not fill the status automatically.
		// update the required status field of the deployment to overcome the failure and requeue.
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
		}
		deployment.Status.AvailableReplicas = 1
		err = r.Status().Update(context.TODO(), deployment)
		assert.NoError(t, err)

		// call ensureCreated again after filling the status of deployment, It will pass now
		res, err = obj.ensureCreated(r, instance)
		assert.NoError(t, err)
		assert.True(t, res.IsZero())

		deployment = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
		}
		assert.NoError(t, r.Client.Get(context.TODO(), client.ObjectKeyFromObject(deployment), deployment))
		expectedDeployment := GetProviderAPIServerDeploymentForTest(instance)
		assert.Equal(t, deployment.Spec, expectedDeployment.Spec)

		service = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
		}
		assert.NoError(t, r.Client.Get(context.TODO(), client.ObjectKeyFromObject(service), service))
		expectedService := GetProviderAPIServerServiceForTest(instance)
		assert.Equal(t, expectedService.Spec, service.Spec)
	})

	t.Run("Ensure that Deployment,Service is created when ProviderAPIServerServiceType set to loadBalancer", func(t *testing.T) {

		r, instance := createSetupForOcsProviderTest(t, corev1.ServiceTypeLoadBalancer)

		obj := &ocsProviderServer{}
		res, err := obj.ensureCreated(r, instance)
		assert.NoError(t, err)
		assert.False(t, res.IsZero())

		// storagecluster controller waits for svc status to fetch the IP and it requeue
		// as we are using a fake client and it does not fill the status automatically.
		// update the required status field of the svc to overcome the failure and requeue.
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
		}
		service.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
			{
				Hostname: "fake",
			},
		}
		err = r.Status().Update(context.TODO(), service)
		assert.NoError(t, err)

		// call ensureCreated again after filling the status of svc, It will fail on deployment now
		res, err = obj.ensureCreated(r, instance)
		assert.NoError(t, err)
		assert.False(t, res.IsZero())

		// storagecluster controller waits for deployment status to fetch the replica count and it requeue
		// as we are using a fake client and it does not fill the status automatically.
		// update the required status field of the deployment to overcome the failure and requeue.
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
		}
		deployment.Status.AvailableReplicas = 1
		err = r.Status().Update(context.TODO(), deployment)
		assert.NoError(t, err)

		// call ensureCreated again after filling the status of deployment, It will pass now
		res, err = obj.ensureCreated(r, instance)
		assert.NoError(t, err)
		assert.True(t, res.IsZero())

		deployment = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
		}
		assert.NoError(t, r.Client.Get(context.TODO(), client.ObjectKeyFromObject(deployment), deployment))
		expectedDeployment := GetProviderAPIServerDeploymentForTest(instance)
		assert.Equal(t, deployment.Spec, expectedDeployment.Spec)

		service = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
		}
		assert.NoError(t, r.Client.Get(context.TODO(), client.ObjectKeyFromObject(service), service))
		expectedService := GetLoadBalancerProviderAPIServerServiceForTest(instance)
		assert.Equal(t, expectedService.Spec, service.Spec)
	})

	t.Run("Ensure that Deployment,Service is created when ProviderAPIServerServiceType set to ClusterIP", func(t *testing.T) {

		r, instance := createSetupForOcsProviderTest(t, corev1.ServiceTypeClusterIP)

		obj := &ocsProviderServer{}
		res, err := obj.ensureCreated(r, instance)
		assert.NoError(t, err)
		assert.False(t, res.IsZero())

		// storagecluster controller waits for svc status to fetch the IP and it requeue
		// as we are using a fake client and it does not fill the status automatically.
		// update the required status field of the svc to overcome the failure and requeue.
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
		}
		err = r.Update(context.TODO(), service)
		assert.NoError(t, err)

		// call ensureCreated again after filling the status of svc, It will fail on deployment now
		res, err = obj.ensureCreated(r, instance)
		assert.NoError(t, err)
		assert.False(t, res.IsZero())

		// storagecluster controller waits for deployment status to fetch the replica count and it requeue
		// as we are using a fake client and it does not fill the status automatically.
		// update the required status field of the deployment to overcome the failure and requeue.
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
		}
		deployment.Status.AvailableReplicas = 1
		err = r.Status().Update(context.TODO(), deployment)
		assert.NoError(t, err)

		// call ensureCreated again after filling the status of deployment, It will pass now
		res, err = obj.ensureCreated(r, instance)
		assert.NoError(t, err)
		assert.True(t, res.IsZero())

		deployment = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
		}
		assert.NoError(t, r.Client.Get(context.TODO(), client.ObjectKeyFromObject(deployment), deployment))
		expectedDeployment := GetProviderAPIServerDeploymentForTest(instance)
		assert.Equal(t, deployment.Spec, expectedDeployment.Spec)

		service = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
		}
		assert.NoError(t, r.Client.Get(context.TODO(), client.ObjectKeyFromObject(service), service))
		expectedService := GetClusterIPProviderAPIServerServiceForTest(instance)
		assert.Equal(t, expectedService.Spec, service.Spec)
	})

	t.Run("Ensure that Service is not created when ProviderAPIServerServiceType is set to any other value than NodePort, ClusterIP or LoadBalancer", func(t *testing.T) {

		r, instance := createSetupForOcsProviderTest(t, corev1.ServiceTypeExternalName)

		obj := &ocsProviderServer{}
		_, err := obj.ensureCreated(r, instance)
		assert.Errorf(t, err, "only supports service of type")
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
		}
		assert.True(t, errors.IsNotFound(r.Client.Get(context.TODO(), client.ObjectKeyFromObject(service), service)))
	})
}

func TestOcsProviderServerEnsureDeleted(t *testing.T) {

	t.Run("Ensure that Deployment,Service is deleted while uninstalling", func(t *testing.T) {

		r, instance := createSetupForOcsProviderTest(t, "")
		obj := &ocsProviderServer{}
		// create resources and ignore error as it should be tested via TestOcsProviderServerEnsureCreated
		_, _ = obj.ensureCreated(r, instance)

		_, err := obj.ensureDeleted(r, instance)
		assert.NoError(t, err)

		assertNotFoundProviderResources(t, r.Client)
	})
}

func assertNotFoundProviderResources(t *testing.T, cli client.Client) {

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
	}
	assert.True(t, errors.IsNotFound(cli.Get(context.TODO(), client.ObjectKeyFromObject(deployment), deployment)))

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
	}
	assert.True(t, errors.IsNotFound(cli.Get(context.TODO(), client.ObjectKeyFromObject(service), service)))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
	}
	assert.True(t, errors.IsNotFound(cli.Get(context.TODO(), client.ObjectKeyFromObject(secret), secret)))

}

func createSetupForOcsProviderTest(t *testing.T, providerAPIServerServiceType corev1.ServiceType) (*StorageClusterReconciler, *ocsv1.StorageCluster) {

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"node-role.kubernetes.io/worker": "",
			},
		},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "0:0:0:0",
				},
			},
		},
	}

	clientConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: ocsClientConfigMapName},
	}

	scheme := createFakeScheme(t)

	frecorder := record.NewFakeRecorder(1024)
	reporter := util.NewEventReporter(frecorder)

	r := &StorageClusterReconciler{
		recorder: reporter,
		Client:   fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(node, clientConfigMap).Build(),
		Scheme:   scheme,
		Log:      logf.Log.WithName("controller_storagecluster_test"),
	}

	instance := &ocsv1.StorageCluster{
		Spec: ocsv1.StorageClusterSpec{
			ProviderAPIServerServiceType: providerAPIServerServiceType,
		},
	}

	os.Setenv(providerAPIServerImage, "fake-image")
	os.Setenv(onboardingValidationKeysGeneratorImage, "fake-image")

	return r, instance
}

func GetProviderAPIServerDeploymentForTest(instance *ocsv1.StorageCluster) *appsv1.Deployment {

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
									Name: util.PodNamespaceEnvVar,
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.namespace",
										},
									},
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
									MountPath: util.ProviderCertsMountPoint,
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
					Tolerations:        getPlacement(instance, defaults.APIServerKey).Tolerations,
				},
			},
		},
	}
}

func GetProviderAPIServerServiceForTest(instance *ocsv1.StorageCluster) *corev1.Service {

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
					NodePort:   ocsProviderServiceNodePort,
					Port:       ocsProviderServicePort,
					TargetPort: intstr.FromString("ocs-provider"),
				},
			},
			Type: corev1.ServiceTypeNodePort,
		},
	}
}

func GetLoadBalancerProviderAPIServerServiceForTest(instance *ocsv1.StorageCluster) *corev1.Service {

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
					NodePort:   ocsProviderServiceNodePort,
					Port:       ocsProviderServicePort,
					TargetPort: intstr.FromString("ocs-provider"),
				},
			},
			Type: corev1.ServiceTypeLoadBalancer,
		},
	}
}

func GetClusterIPProviderAPIServerServiceForTest(instance *ocsv1.StorageCluster) *corev1.Service {

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
					Port:       ocsProviderServicePort,
					TargetPort: intstr.FromString("ocs-provider"),
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
}
