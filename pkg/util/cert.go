package util

import (
	"context"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// LoadPeerCertificates loads and validates mTLS certificates for a StorageClusterPeer.
// Returns serverCA, clientCert, clientKey as byte slices (nil if not configured).
// Returns error if secret is configured but missing or empty.
func LoadPeerCertificates(
	ctx context.Context,
	k8sClient client.Client,
	peer *ocsv1.StorageClusterPeer,
) (serverCA, clientCert, clientKey []byte, err error) {
	// Load server CA certificate if configured
	if peer.Spec.ServerCASecret != nil && peer.Spec.ServerCASecret.Name != "" {
		secret := &corev1.Secret{}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      peer.Spec.ServerCASecret.Name,
			Namespace: peer.Namespace,
		}, secret)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to get server CA secret %s: %w", peer.Spec.ServerCASecret.Name, err)
		}
		serverCA = secret.Data["ca.crt"]
		if len(serverCA) == 0 {
			return nil, nil, nil, fmt.Errorf("ca.crt not found in server CA secret %s", peer.Spec.ServerCASecret.Name)
		}
	}

	// Load client certificate if configured
	if peer.Spec.ClientCertSecret != nil && peer.Spec.ClientCertSecret.Name != "" {
		secret := &corev1.Secret{}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      peer.Spec.ClientCertSecret.Name,
			Namespace: peer.Namespace,
		}, secret)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to get client cert secret %s: %w", peer.Spec.ClientCertSecret.Name, err)
		}
		clientCert = secret.Data["tls.crt"]
		clientKey = secret.Data["tls.key"]
		if len(clientCert) == 0 || len(clientKey) == 0 {
			return nil, nil, nil, fmt.Errorf("tls.crt or tls.key not found in client cert secret %s", peer.Spec.ClientCertSecret.Name)
		}
	}

	return serverCA, clientCert, clientKey, nil
}
