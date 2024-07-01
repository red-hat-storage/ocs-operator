package server

import (
	"context"
	"fmt"
	"slices"

	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type cephBlockPoolManager struct {
	client    client.Client
	namespace string
}

func newCephBlockPoolManager(cl client.Client, namespace string) (*cephBlockPoolManager, error) {
	return &cephBlockPoolManager{
		client:    cl,
		namespace: namespace,
	}, nil
}

func (c *cephBlockPoolManager) EnableBlockPoolMirroring(ctx context.Context, cephBlockPool *rookCephv1.CephBlockPool) error {

	cephBlockPool.Spec.Mirroring.Enabled = true
	cephBlockPool.Spec.Mirroring.Mode = "image"

	err := c.client.Update(ctx, cephBlockPool)
	if err != nil {
		return fmt.Errorf("failed to enable mirroring on CephBlockPool resource with name %q: %v", cephBlockPool.Name, err)
	}

	return nil

}

func (c *cephBlockPoolManager) SetBootstrapSecretRef(ctx context.Context, cephBlockPool *rookCephv1.CephBlockPool, secretName string, secretData map[string][]byte) error {

	// create the secret
	bootstrapSecret := &corev1.Secret{}
	bootstrapSecret.Name = secretName
	bootstrapSecret.Namespace = c.namespace

	_, err := ctrl.CreateOrUpdate(ctx, c.client, bootstrapSecret, func() error {
		bootstrapSecret.Data = secretData
		return ctrl.SetControllerReference(cephBlockPool, bootstrapSecret, c.client.Scheme())
	})
	if err != nil {
		return fmt.Errorf("failed to create/update the bootstrap secret %q: %v", secretName, err)
	}

	// set the secret ref
	if cephBlockPool.Spec.Mirroring.Peers == nil {
		cephBlockPool.Spec.Mirroring.Peers = &rookCephv1.MirroringPeerSpec{SecretNames: []string{secretName}}
	} else {
		if !slices.Contains(cephBlockPool.Spec.Mirroring.Peers.SecretNames, secretName) {
			cephBlockPool.Spec.Mirroring.Peers.SecretNames = append(cephBlockPool.Spec.Mirroring.Peers.SecretNames, secretName)
		}
	}

	err = c.client.Update(ctx, cephBlockPool)
	if err != nil {
		return fmt.Errorf("failed to set bootstrap secret ref on CephBlockPool resource with name %q: %v", cephBlockPool.Name, err)
	}

	return nil
}

func (c *cephBlockPoolManager) UnSetAndDeleteBootstrapSecret(ctx context.Context, secretName string, cephBlockPool *rookCephv1.CephBlockPool) error {

	// remove the secret ref
	index := slices.IndexFunc(cephBlockPool.Spec.Mirroring.Peers.SecretNames, func(s string) bool {
		return s == secretName
	})
	if index >= 0 {
		cephBlockPool.Spec.Mirroring.Peers.SecretNames = append(
			cephBlockPool.Spec.Mirroring.Peers.SecretNames[:index],
			cephBlockPool.Spec.Mirroring.Peers.SecretNames[index+1:]...)
	}

	err := c.client.Update(ctx, cephBlockPool)
	if err != nil {
		return fmt.Errorf("failed to unset bootstrap secret ref on CephBlockPool resource with name %q: %v", cephBlockPool.Name, err)
	}

	// delete secret
	bootstrapSecret := &corev1.Secret{}
	bootstrapSecret.Name = secretName
	bootstrapSecret.Namespace = c.namespace
	err = c.client.Delete(ctx, bootstrapSecret)
	// there might be a case where the bootstrap secret was deleted but request failed after this and there was a retry,
	// if error is IsNotFound, that means it is safe to proceed as we have deleted the bootstrap secret
	if err != nil && !kerrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete the bootstrap secret %q: %v", secretName, err)
	}
	return nil
}

func (c *cephBlockPoolManager) DisableBlockPoolMirroring(ctx context.Context, cephBlockPool *rookCephv1.CephBlockPool) error {

	// disable only if no bootstrap secret has been set
	if cephBlockPool.Spec.Mirroring.Peers == nil || len(cephBlockPool.Spec.Mirroring.Peers.SecretNames) == 0 {
		cephBlockPool.Spec.Mirroring.Enabled = false
		cephBlockPool.Spec.Mirroring.Mode = ""
	}

	err := c.client.Update(ctx, cephBlockPool)
	if err != nil {
		return fmt.Errorf("failed to disable mirroring on CephBlockPool resource with name %q: %v", cephBlockPool.Name, err)
	}

	return nil
}

func (c *cephBlockPoolManager) GetBlockPoolByName(ctx context.Context, blockPoolName string) (*rookCephv1.CephBlockPool, error) {
	blockPool := &rookCephv1.CephBlockPool{}
	blockPool.Name = blockPoolName
	blockPool.Namespace = c.namespace
	err := c.client.Get(ctx, client.ObjectKeyFromObject(blockPool), blockPool)
	if err != nil {
		return nil, fmt.Errorf("failed to get CephBlockPool resource with name %q: %v", blockPoolName, err)
	}
	return blockPool, nil
}

// IsRBDMirrorRequired checks if we require RBDMirror to be deployed or not
func (c *cephBlockPoolManager) IsRBDMirrorRequired(ctx context.Context) (bool, error) {
	cephBlockPoolList := &rookCephv1.CephBlockPoolList{}
	err := c.client.List(ctx, cephBlockPoolList, client.InNamespace(c.namespace))
	if err != nil {
		return true, err
	}

	// if we find a bootstrap secret in any of the blockPools, we require RBDMirror to be deployed
	for _, cephBlockPool := range cephBlockPoolList.Items {
		if cephBlockPool.Spec.Mirroring.Peers != nil && len(cephBlockPool.Spec.Mirroring.Peers.SecretNames) > 0 {
			return true, nil
		}
	}
	return false, nil
}
