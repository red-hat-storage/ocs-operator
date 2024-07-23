package util

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ReadPrivateKey(cl client.Client) (*rsa.PrivateKey, error) {
	klog.Info("Getting the Pem key")
	ctx := context.Background()

	operatorNamespace, err := GetOperatorNamespace()
	if err != nil {
		return nil, fmt.Errorf("unable to get operator namespace: %v", err)
	}

	privateSecret := &corev1.Secret{}
	privateSecret.Name = onboardingValidationPrivateKeySecretName
	privateSecret.Namespace = operatorNamespace

	err = cl.Get(ctx, client.ObjectKeyFromObject(privateSecret), privateSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to get private secret: %v", err)
	}

	Block, _ := pem.Decode(privateSecret.Data["key"])
	privateKey, err := x509.ParsePKCS1PrivateKey(Block.Bytes)

	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %v", err)
	}

	return privateKey, nil
}
