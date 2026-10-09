package storagecluster

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"maps"
	"math/big"
	"strconv"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"

	ocsclientv1a1 "github.com/red-hat-storage/ocs-client-operator/api/v1alpha1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	ocsClientConfigMapName            = "ocs-client-operator-config"
	manageNoobaaSubKey                = "manageNoobaaSubscription"
	disableVersionChecksKey           = "disableVersionChecks"
	disableInstallPlanAutoApprovalKey = "disableInstallPlanAutoApproval"
	// disableS3EndpointProxyKey disables deploying the S3 endpoint reverse proxy for the local/internal client
	disableS3EndpointProxyKey = "disableS3EndpointProxy"
	// cephNetworkAnnotationKey is the annotation key used to store network details used by ceph
	cniNetworksAnnotationKey = "k8s.v1.cni.cncf.io/networks"
	// clientCertSecretName is the name of the secret containing the mTLS client certificate
	clientCertSecretName = "ocs-internal-client-certificate"
)

type storageClient struct{}

var _ resourceManager = &storageClient{}

func (s *storageClient) ensureCreated(r *StorageClusterReconciler, storagecluster *ocsv1.StorageCluster) (reconcile.Result, error) {

	if err := s.updateClientConfigMap(r, storagecluster.Namespace); err != nil {
		return reconcile.Result{}, err
	}

	clientCertSecret := &corev1.Secret{}
	clientCertSecret.Name = clientCertSecretName
	clientCertSecret.Namespace = storagecluster.Namespace

	err := r.Get(r.ctx, client.ObjectKeyFromObject(clientCertSecret), clientCertSecret)

	if err != nil && !kerrors.IsNotFound(err) {
		return reconcile.Result{}, fmt.Errorf("failed to check client cert: %v", err)
	}

	if kerrors.IsNotFound(err) || !s.isClientCertValid(clientCertSecret) {
		if err := s.generateClientCertificate(r, storagecluster, clientCertSecret); err != nil {
			return reconcile.Result{}, err
		}
	}

	storageClient := &ocsclientv1a1.StorageClient{}
	storageClient.Name = storagecluster.Name
	_, err = controllerutil.CreateOrUpdate(r.ctx, r.Client, storageClient, func() error {
		if storageClient.Status.ConsumerID == "" {
			localStorageConsumer := &ocsv1a1.StorageConsumer{}
			localStorageConsumer.Name = defaults.LocalStorageConsumerName
			localStorageConsumer.Namespace = storagecluster.Namespace
			if err := r.Get(r.ctx, client.ObjectKeyFromObject(localStorageConsumer), localStorageConsumer); err != nil {
				return fmt.Errorf("failed to get storageconsumer %s: %v", localStorageConsumer.Name, err)
			} else if localStorageConsumer.Status.OnboardingTicketSecret.Name == "" {
				return fmt.Errorf("no reference to onboarding secret found in storageconsumer %s status", localStorageConsumer.Name)
			}

			onboardingSecret := &corev1.Secret{}
			onboardingSecret.Name = localStorageConsumer.Status.OnboardingTicketSecret.Name
			onboardingSecret.Namespace = storagecluster.Namespace
			if err := r.Get(r.ctx, client.ObjectKeyFromObject(onboardingSecret), onboardingSecret); err != nil {
				return fmt.Errorf("failed to get onboarding secret %s: %v", onboardingSecret.Name, err)
			} else if len(onboardingSecret.Data[defaults.OnboardingTokenKey]) == 0 {
				return fmt.Errorf("no 'onboarding-token' field found in onboarding secret %s", onboardingSecret.Name)
			}

			storageClient.Spec.OnboardingTicket = string(onboardingSecret.Data[defaults.OnboardingTokenKey])
		}
		// we could just use svcName:port however in-cluster traffic from "*.svc" is generally not proxied and
		// we using qualified name upto ".svc" makes connection not go through any proxies.
		storageClient.Spec.StorageProviderEndpoint = fmt.Sprintf("%s.%s.svc:%d", ocsProviderServerName, storagecluster.Namespace, ocsProviderServicePort)

		cephNWAnnotationValue, err := getCephNetworkAnnotationValue(storagecluster.Spec.Network, storagecluster.Namespace)
		if err != nil {
			return fmt.Errorf("failed to get Ceph network annotation value: %v", err)
		}
		if cephNWAnnotationValue != "" {
			util.AddAnnotation(storageClient, cniNetworksAnnotationKey, cephNWAnnotationValue)
		}

		controllerutil.AddFinalizer(storageClient, internalComponentFinalizer)

		return nil
	})
	if err != nil {
		r.Log.Error(err, "Failed to create local StorageClient CR")
		return reconcile.Result{}, nil
	}

	return reconcile.Result{}, nil
}

func (s *storageClient) ensureDeleted(r *StorageClusterReconciler, storagecluster *ocsv1.StorageCluster) (reconcile.Result, error) {
	storageClient := &ocsclientv1a1.StorageClient{}
	storageClient.Name = storagecluster.Name
	if err := r.Get(r.ctx, client.ObjectKeyFromObject(storageClient), storageClient); client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get storageclient %s: %v", storageClient.Name, err)
	} else if storageClient.UID == "" {
		return reconcile.Result{}, nil
	}

	certSecret := &corev1.Secret{}
	certSecret.Name = clientCertSecretName
	certSecret.Namespace = storagecluster.Namespace
	if err := r.Delete(r.ctx, certSecret); client.IgnoreNotFound(err) != nil {
		r.Log.Error(err, "Failed to delete client cert secret")
	}

	if err := r.Delete(r.ctx, storageClient); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to delete storageclient %s: %v", storageClient.Name, err)
	}

	if controllerutil.RemoveFinalizer(storageClient, internalComponentFinalizer) {
		r.Log.Info("Removing finalizer from StorageClient.", "StorageClient:", storageClient.Name, " Finalizer:", internalComponentFinalizer)
		if err := r.Update(r.ctx, storageClient); err != nil {
			r.Log.Info("Failed to remove finalizer from StorageClient.", "StorageClient:", storageClient.Name, " Finalizer:", internalComponentFinalizer)
			return reconcile.Result{}, fmt.Errorf("failed to remove finalizer from StorageClient: %v", err)
		}
	}
	return reconcile.Result{}, nil
}

func (s *storageClient) updateClientConfigMap(r *StorageClusterReconciler, namespace string) error {
	clientConfig := &corev1.ConfigMap{}
	clientConfig.Name = ocsClientConfigMapName
	clientConfig.Namespace = namespace

	if err := r.Get(r.ctx, client.ObjectKeyFromObject(clientConfig), clientConfig); err != nil {
		r.Log.Error(err, "failed to get ocs client configmap")
		return err
	}

	existingData := maps.Clone(clientConfig.Data)
	if clientConfig.Data == nil {
		clientConfig.Data = map[string]string{}
	}
	clientConfig.Data[manageNoobaaSubKey] = strconv.FormatBool(false)
	clientConfig.Data[disableVersionChecksKey] = strconv.FormatBool(true)
	clientConfig.Data[disableInstallPlanAutoApprovalKey] = strconv.FormatBool(true)
	clientConfig.Data[disableS3EndpointProxyKey] = strconv.FormatBool(true)

	if !maps.Equal(clientConfig.Data, existingData) {
		if err := r.Update(r.ctx, clientConfig); err != nil {
			r.Log.Error(err, "failed to update client operator's configmap data")
			return err
		}
	}

	return nil
}

// getCephNetworkAnnotationValue returns the network annotation value for the given StorageCluster NetworkSpec.
func getCephNetworkAnnotationValue(cephNetworkSpec *rookCephv1.NetworkSpec, scNamespace string) (string, error) {
	if cephNetworkSpec == nil {
		return "", nil // cannot be multus if no network spec
	}
	if !cephNetworkSpec.IsMultus() {
		return "", nil // if not multus, no annotation to add
	}
	if len(cephNetworkSpec.Selectors) == 0 {
		return "", fmt.Errorf("invalid ceph network spec")
	}

	networkSelectionElement, err := cephNetworkSpec.GetNetworkSelection(scNamespace, rookCephv1.CephNetworkType("public"))
	if err != nil {
		return "", fmt.Errorf("failed to get network selection element: %v", err)
	}
	if networkSelectionElement == nil {
		return "", nil // annotation only needed for clusters w/ multus public net selected
	}

	nwAnnotation, err := rookCephv1.NetworkSelectionsToAnnotationValue(networkSelectionElement)
	if err != nil {
		return "", err
	}
	return nwAnnotation, nil
}

// isClientCertValid checks if the client certificate in the secret is still valid
func (s *storageClient) isClientCertValid(secret *corev1.Secret) bool {
	certPEM := secret.Data["tls.crt"]
	if certPEM == nil {
		return false
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return false
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}

	// Check expiry (30 day renewal window)
	now := time.Now()
	renewalThreshold := cert.NotAfter.Add(-30 * 24 * time.Hour)

	if now.Before(cert.NotBefore) || now.After(renewalThreshold) {
		return false
	}

	for _, eku := range cert.ExtKeyUsage {
		if eku == x509.ExtKeyUsageClientAuth {
			return true
		}
	}

	return false
}

// generateClientCertificate creates a new self-signed client certificate for mTLS
func (s *storageClient) generateClientCertificate(r *StorageClusterReconciler, storagecluster *ocsv1.StorageCluster, clientCertSecret *corev1.Secret) error {
	client_name := fmt.Sprintf("ocs-client-%d", time.Now().Unix())
	r.Log.Info("Generating client certificate", "client_name", client_name)

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate key: %v", err)
	}

	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))

	certTemplate := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: client_name,
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(1, 0, 0),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, certTemplate, certTemplate, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	_, err = controllerutil.CreateOrUpdate(r.ctx, r.Client, clientCertSecret, func() error {
		if clientCertSecret.Data == nil {
			clientCertSecret.Data = make(map[string][]byte)
		}
		clientCertSecret.Type = corev1.SecretTypeTLS
		clientCertSecret.Data["tls.crt"] = certPEM
		clientCertSecret.Data["tls.key"] = keyPEM
		clientCertSecret.Data["ca.crt"] = certPEM

		return controllerutil.SetOwnerReference(storagecluster, clientCertSecret, r.Scheme)
	})

	if err != nil {
		return fmt.Errorf("failed to create client cert secret: %v", err)
	}

	r.Log.Info("Client certificate created", "client_name", client_name)
	return nil
}
