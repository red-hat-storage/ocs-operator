package server

import (
	"context"

	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const rBDMirrorName = "rbd-mirror"

type cephRBDMirrorManager struct {
	client    client.Client
	namespace string
}

func newCephRBDMirrorManager(cl client.Client, namespace string) (*cephRBDMirrorManager, error) {
	return &cephRBDMirrorManager{
		client:    cl,
		namespace: namespace,
	}, nil
}

func (c *cephRBDMirrorManager) Create(ctx context.Context) error {

	cephRBDMirror := &rookCephv1.CephRBDMirror{}
	cephRBDMirror.Name = rBDMirrorName
	cephRBDMirror.Namespace = c.namespace
	err := c.client.Get(ctx, client.ObjectKeyFromObject(cephRBDMirror), cephRBDMirror)

	// create if not found
	if err != nil && kerrors.IsNotFound(err) {
		cephRBDMirror.Spec = rookCephv1.RBDMirroringSpec{Count: 1}
		err = c.client.Create(ctx, cephRBDMirror)
		if err != nil {
			return err
		}
	}

	// if any other err/nil return it
	return err
}
