package storagecluster

import (
	"crypto/x509"
	"fmt"
	"os"
	"strconv"
	"time"

	"go.uber.org/multierr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"
	"github.com/red-hat-storage/ocs-operator/v4/services/provider/server"

	routev1 "github.com/openshift/api/route/v1"
	secv1 "github.com/openshift/api/security/v1"
)

const (
	ocsProviderServerName                    = defaults.OcsProviderServerName
	providerAPIServerImage                   = "PROVIDER_API_SERVER_IMAGE"
	onboardingValidationKeysGeneratorImage   = "ONBOARDING_VALIDATION_KEYS_GENERATOR_IMAGE"
	onboardingValidationKeysGeneratorJobName = "onboarding-validation-keys-generator"
	onboardingValidationPublicKeySecretName  = "onboarding-ticket-key"

	ocsProviderServicePort     = int32(50051)
	ocsProviderServiceNodePort = int32(31659)

	ocsProviderCertSecretName = ocsProviderServerName + "-cert"

	clientCertValidity      = util.SelfSignedCertValidity
	clientCertRenewalWindow = 7 * 24 * time.Hour
)

type ocsProviderServer struct{}

func (o *ocsProviderServer) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {

	if res, err := o.createService(r, instance); err != nil || !res.IsZero() {
		return res, err
	}

	if res, err := o.createRoute(r, instance); err != nil || !res.IsZero() {
		return res, err
	}

	if res, err := o.createNetworkPolicy(r, instance); err != nil || !res.IsZero() {
		return res, err
	}

	if res, err := o.createClientCertAndCA(r, instance); err != nil || !res.IsZero() {
		return res, err
	}

	if res, err := o.createDeployment(r, instance); err != nil || !res.IsZero() {
		return res, err
	}

	if res, err := o.createJob(r, instance); err != nil || !res.IsZero() {
		return res, err
	}

	return reconcile.Result{}, nil
}

func (o *ocsProviderServer) createRoute(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	serverRoute := &routev1.Route{}
	serverRoute.Name = ocsProviderServerName
	serverRoute.Namespace = instance.Namespace
	_, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, serverRoute, func() error {
		serverRoute.Spec = routev1.RouteSpec{
			Subdomain: fmt.Sprintf("ocs-provider-%s", serverRoute.Namespace),
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: ocsProviderServerName,
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString("ocs-provider"),
			},
			TLS: &routev1.TLSConfig{
				Termination: routev1.TLSTerminationPassthrough,
			},
		}
		return controllerutil.SetControllerReference(instance, serverRoute, r.Client.Scheme())
	})
	if err != nil {
		r.Log.Error(err, "Failed to create/update route", "Name", serverRoute.Name)
		return reconcile.Result{}, err
	}

	host, admitted := util.GetAdmittedRouteHost(serverRoute)
	if admitted {
		instance.Status.StorageProviderEndpoint = fmt.Sprintf("%s:443", host)
	} else {
		if time.Now().After(serverRoute.CreationTimestamp.Add(5 * time.Minute)) {
			return reconcile.Result{}, fmt.Errorf("no admitted route found for %s", serverRoute.Name)
		}
		return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
	}

	return reconcile.Result{}, nil
}

// TODO: in next version, have the networkpolicy only to allow via svc (own namespace) or via route
func (o *ocsProviderServer) createNetworkPolicy(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	desiredService := GetProviderAPIServerService(instance)

	networkPolicy := &networkingv1.NetworkPolicy{}
	networkPolicy.Name = ocsProviderServerName
	networkPolicy.Namespace = instance.Namespace

	if err := r.Get(r.ctx, client.ObjectKeyFromObject(networkPolicy), networkPolicy); client.IgnoreNotFound(err) != nil {
		r.Log.Error(err, "Failed to get networkpolicy", "Name", networkPolicy.Name)
		return reconcile.Result{}, err
	}

	// Cross-cluster DR traffic via Submariner/Globalnet cannot match any
	// namespaceSelector, so the NetworkPolicy must not exist when a peer is present.
	hasPeers, err := hasStorageClusterPeers(r, instance.Namespace)
	if err != nil {
		r.Log.Error(err, "Failed to list StorageClusterPeer resources")
		return reconcile.Result{}, err
	}

	if desiredService.Spec.Type != corev1.ServiceTypeClusterIP || hasPeers {
		if networkPolicy.UID != "" {
			if err := r.Delete(r.ctx, networkPolicy); err != nil {
				r.Log.Error(err, "Failed to delete networkpolicy", "Name", networkPolicy.Name)
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}

	desiredNetworkPolicy := getProviderAPIServerNetworkPolicy(instance)

	if _, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, networkPolicy, func() error {
		networkPolicy.Spec = desiredNetworkPolicy.Spec
		return controllerutil.SetControllerReference(instance, networkPolicy, r.Client.Scheme())
	}); err != nil {
		r.Log.Error(err, "Failed to create/update networkpolicy", "Name", desiredNetworkPolicy.Name)
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func hasStorageClusterPeers(r *StorageClusterReconciler, namespace string) (bool, error) {
	peerList := &metav1.PartialObjectMetadataList{}
	peerList.SetGroupVersionKind(ocsv1.GroupVersion.WithKind("StorageClusterPeerList"))
	if err := r.List(r.ctx, peerList, client.InNamespace(namespace), client.Limit(1)); err != nil {
		return false, err
	}
	return len(peerList.Items) > 0, nil
}

func (o *ocsProviderServer) ensureDeleted(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	if err := r.verifyNoStorageConsumerExist(instance); err != nil {
		return reconcile.Result{}, err
	}

	var finalErr error

	apiServerService := &corev1.Service{}
	apiServerService.Name = ocsProviderServerName
	apiServerService.Namespace = instance.Namespace
	apiServerNetworkPolicy := &networkingv1.NetworkPolicy{}
	apiServerNetworkPolicy.Name = ocsProviderServerName
	apiServerNetworkPolicy.Namespace = instance.Namespace
	apiServerRoute := &routev1.Route{}
	apiServerRoute.Name = ocsProviderServerName
	apiServerRoute.Namespace = instance.Namespace
	for _, resource := range []client.Object{
		apiServerRoute,
		apiServerService,
		apiServerNetworkPolicy,
		GetProviderAPIServerDeployment(instance),
	} {
		err := r.Delete(r.ctx, resource)

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

	for _, env := range []string{providerAPIServerImage} {
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

	_, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, actualDeployment, func() error {
		actualDeployment.Spec = desiredDeployment.Spec
		return controllerutil.SetOwnerReference(instance, actualDeployment, r.Client.Scheme())
	})
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

	desiredService := GetProviderAPIServerService(instance)
	actualService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desiredService.Name,
			Namespace: desiredService.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, actualService, func() error {
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
	})
	if err != nil {
		r.Log.Error(err, "Failed to create/update service", "Name", desiredService.Name)
		return reconcile.Result{}, err
	}

	r.Log.Info("Service create/update succeeded")

	switch desiredService.Spec.Type {
	case corev1.ServiceTypeLoadBalancer:
		endpoint := o.getLoadBalancerServiceEndpoint(actualService)

		if endpoint == "" {
			r.recorder.ReportIfNotPresent(instance, corev1.EventTypeNormal, "Waiting", "Waiting for Ingress on service "+actualService.Name)
			r.Log.Info("Waiting for Ingress on service", "Service", actualService.Name, "Status", actualService.Status)
			return reconcile.Result{RequeueAfter: time.Second * 5}, nil
		}

		instance.Status.StorageProviderEndpoint = fmt.Sprintf("%s:%d", endpoint, ocsProviderServicePort)
	}

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

func (o *ocsProviderServer) ensureDeploymentReplica(actual, desired *appsv1.Deployment) error {

	if actual.Status.AvailableReplicas != *desired.Spec.Replicas {
		return fmt.Errorf("deployment %s is not ready", desired.Name)
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
					Annotations: map[string]string{
						secv1.RequiredSCCAnnotation: util.RestrictedV2SccName,
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
								{
									Name:  util.OperatorNamespaceEnvVar,
									Value: instance.Namespace,
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
							Resources: getDaemonResources(ocsProviderServerName, instance),
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
					Tolerations: GetPlacement(instance, defaults.APIServerKey).Tolerations,
					Affinity: &corev1.Affinity{
						NodeAffinity: GetPlacement(instance, defaults.APIServerKey).NodeAffinity,
					},
					PriorityClassName:  systemClusterCritical,
					ServiceAccountName: ocsProviderServerName,
				},
			},
		},
	}
}

func GetProviderAPIServerService(instance *ocsv1.StorageCluster) *corev1.Service {
	serviceType := corev1.ServiceTypeClusterIP
	if instance.Spec.ProviderAPIServerServiceType != "" {
		serviceType = instance.Spec.ProviderAPIServerServiceType
	} else if instance.Spec.HostNetwork {
		serviceType = corev1.ServiceTypeNodePort
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
						if serviceType == corev1.ServiceTypeClusterIP {
							return 0
						}
						return ocsProviderServiceNodePort
					}(),
					Port:       ocsProviderServicePort,
					TargetPort: intstr.FromString("ocs-provider"),
				},
			},
			Type: serviceType,
		},
	}
}

func getProviderAPIServerNetworkPolicy(instance *ocsv1.StorageCluster) *networkingv1.NetworkPolicy {

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ocsProviderServerName,
			Namespace: instance.Namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "ocsProviderApiServer",
				},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": instance.Namespace,
								},
							},
						},
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									// documented as preferred for ovn-k
									"policy-group.network.openshift.io/ingress": "",
								},
							},
						},
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									// still appears in docs
									"network.openshift.io/policy-group": "ingress",
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Protocol: ptr.To(corev1.ProtocolTCP),
							Port:     ptr.To(intstr.FromInt32(ocsProviderServicePort)),
						},
					},
				},
			},
		},
	}
}

func getOnboardingJobObject(instance *ocsv1.StorageCluster) *batchv1.Job {

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      onboardingValidationKeysGeneratorJobName,
			Namespace: instance.Namespace,
		},
		Spec: batchv1.JobSpec{
			// Eligible to delete automatically when job finishes
			TTLSecondsAfterFinished: ptr.To(int32(0)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						secv1.RequiredSCCAnnotation: util.RestrictedV2SccName,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyOnFailure,
					ServiceAccountName: onboardingValidationKeysGeneratorJobName,
					Containers: []corev1.Container{
						{
							Name:    onboardingValidationKeysGeneratorJobName,
							Image:   os.Getenv(onboardingValidationKeysGeneratorImage),
							Command: []string{"/usr/local/bin/onboarding-validation-keys-gen"},
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
							SecurityContext: &corev1.SecurityContext{
								RunAsNonRoot:             ptr.To(true),
								AllowPrivilegeEscalation: ptr.To(false),
							},
						},
					},
					// Use the same tolerations as the Provider API Server
					// As the generator job is too insignificant to have it's own placement
					Tolerations: GetPlacement(instance, defaults.APIServerKey).Tolerations,
				},
			},
		},
	}
}

func (o *ocsProviderServer) createJob(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	var err error
	if os.Getenv(onboardingValidationKeysGeneratorImage) == "" {
		err = fmt.Errorf("OnboardingSecretGeneratorImage env var is not set")
		r.Log.Error(err, "No value set for env variable")

		return reconcile.Result{}, err
	}

	actualSecret := &corev1.Secret{}
	// Creating the job only if public is not found
	err = r.Get(r.ctx, types.NamespacedName{Name: onboardingValidationPublicKeySecretName,
		Namespace: instance.Namespace}, actualSecret)

	if errors.IsNotFound(err) {
		onboardingSecretGeneratorJob := getOnboardingJobObject(instance)
		err = r.Create(r.ctx, onboardingSecretGeneratorJob)
	}
	if err != nil {
		r.Log.Error(err, "failed to create/ensure secret")
		return reconcile.Result{}, err
	}

	r.Log.Info("Job is running as desired")
	return reconcile.Result{}, nil
}

func (o *ocsProviderServer) createClientCertAndCA(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	secret := &corev1.Secret{}
	secret.Name = defaults.LocalStorageClientCertSecret
	secret.Namespace = instance.Namespace

	if err := r.Get(r.ctx, client.ObjectKeyFromObject(secret), secret); client.IgnoreNotFound(err) != nil {
		r.Log.Error(err, "Failed to get client cert secret", "Name", secret.Name)
		return reconcile.Result{}, err
	}

	if secret.UID == "" {
		if err := o.populateClientCertSecret(r, instance, secret); err != nil {
			return reconcile.Result{}, err
		}
		if err := r.Create(r.ctx, secret); err != nil {
			if errors.IsAlreadyExists(err) {
				return reconcile.Result{Requeue: true}, nil
			}
			r.Log.Error(err, "Failed to create client cert secret", "Name", secret.Name)
			return reconcile.Result{}, err
		}
	} else if util.CertNeedsRenewal(secret, clientCertRenewalWindow) {
		if err := o.populateClientCertSecret(r, instance, secret); err != nil {
			return reconcile.Result{}, err
		}
		if err := r.Update(r.ctx, secret); err != nil {
			r.Log.Error(err, "Failed to update client cert secret", "Name", secret.Name)
			return reconcile.Result{}, err
		}
	}

	caCert := secret.Data["tls.crt"]
	if len(caCert) == 0 {
		return reconcile.Result{}, fmt.Errorf("client cert secret %s has no tls.crt", defaults.LocalStorageClientCertSecret)
	}
	cm := &corev1.ConfigMap{}
	cm.Name = defaults.LocalStorageClientCAConfigMap
	cm.Namespace = instance.Namespace
	if _, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, cm, func() error {
		if cm.Data == nil {
			cm.Data = make(map[string]string)
		}
		cm.Data["ca.crt"] = string(caCert)
		return controllerutil.SetControllerReference(instance, cm, r.Client.Scheme())
	}); err != nil {
		r.Log.Error(err, "Failed to ensure client CA configmap", "Name", cm.Name)
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

// populateClientCertSecret generates a fresh self-signed client cert with a unique identity.
func (o *ocsProviderServer) populateClientCertSecret(r *StorageClusterReconciler, instance *ocsv1.StorageCluster, secret *corev1.Secret) error {
	clientCN := fmt.Sprintf("%s-%x", defaults.LocalStorageClientCNPrefix, util.FnvHash(strconv.FormatInt(time.Now().UnixNano(), 10)))

	keyUsage := x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature
	extKeyUsage := []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	keyPEM, certPEM, err := util.GetSelfSignedCert(clientCN, keyUsage, extKeyUsage)
	if err != nil {
		return fmt.Errorf("failed to generate client certificate: %v", err)
	}

	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	secret.Data["tls.key"] = keyPEM
	secret.Data["tls.crt"] = certPEM

	if secret.Annotations == nil {
		secret.Annotations = make(map[string]string)
	}
	secret.Annotations[defaults.CertExpiryAnnotation] = time.Now().Add(clientCertValidity).Format(time.RFC3339)
	secret.Annotations[defaults.CertIdentityAnnotation] = clientCN

	secret.Type = corev1.SecretTypeTLS

	if err := controllerutil.SetControllerReference(instance, secret, r.Client.Scheme()); err != nil {
		return err
	}
	r.Log.Info("Generated client certificate", "Name", secret.Name, "Identity", clientCN)
	return nil
}
