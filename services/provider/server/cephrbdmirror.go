package server

import (
	"context"

	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func (c *cephRBDMirrorManager) Delete(ctx context.Context) error {
	cephRBDMirrorObj := &rookCephv1.CephRBDMirror{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rBDMirrorName,
			Namespace: c.namespace,
		},
	}
	err := c.client.Delete(ctx, cephRBDMirrorObj)
	// there might be a case where the RBDMirror was deleted but request failed after this and there was a retry,
	// if error is IsNotFound, that means it is safe to proceed as we have deleted the RBDMirror instance
	if err != nil && !kerrors.IsNotFound(err) {
		return err
	}
	return nil
}
