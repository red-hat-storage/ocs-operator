package storagecluster

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	externalClusterDetailsSecret = "rook-ceph-external-cluster-details"
	externalClusterDetailsKey    = "external_cluster_details"
	cephFsStorageClassName       = "cephfs"
	cephRbdStorageClassName      = "ceph-rbd"
	cephRgwStorageClassName      = "ceph-rgw"
	externalCephRgwEndpointKey   = "endpoint"
)

var (
	// externalRgwEndpoint is the rgw endpoint as discovered in the Secret externalClusterDetailsSecret
	// It is used for independent mode only. It will be passed to the Noobaa CR as a label
	externalRgwEndpoint string
)

// ExternalResource containes a list of External Cluster Resources
type ExternalResource struct {
	Kind string            `json:"kind"`
	Data map[string]string `json:"data"`
	Name string            `json:"name"`
}

// ensureExternalStorageClusterResources ensures that requested resources for the external cluster
// being created
func (r *ReconcileStorageCluster) ensureExternalStorageClusterResources(instance *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	// check for the status boolean value accepted or not
	if instance.Status.ExternalSecretFound {
		return nil
	}
	found := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      externalClusterDetailsSecret,
			Namespace: instance.Namespace,
		},
	}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: found.Name, Namespace: found.Namespace}, found)
	if err != nil {
		return err
	}
	ownerRef := metav1.OwnerReference{
		UID:        instance.UID,
		APIVersion: instance.APIVersion,
		Kind:       instance.Kind,
		Name:       instance.Name,
	}
	var data []ExternalResource
	err = json.Unmarshal(found.Data[externalClusterDetailsKey], &data)
	if err != nil {
		reqLogger.Error(err, "could not parse json blob")
		return err
	}
	scs, err := r.newStorageClasses(instance)
	if err != nil {
		reqLogger.Error(err, "failed to create StorageClasses")
		return err
	}
	for _, d := range data {
		objectMeta := metav1.ObjectMeta{
			Name:            d.Name,
			Namespace:       instance.Namespace,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		}
		objectKey := types.NamespacedName{Name: d.Name, Namespace: instance.Namespace}
		switch d.Kind {
		case "ConfigMap":
			cm := &corev1.ConfigMap{
				ObjectMeta: objectMeta,
				Data:       d.Data,
			}
			found := &corev1.ConfigMap{ObjectMeta: objectMeta}
			err := r.client.Get(context.TODO(), objectKey, found)
			if err != nil {
				if errors.IsNotFound(err) {
					reqLogger.Info(fmt.Sprintf("creating configmap: %s", cm.Name))
					err = r.client.Create(context.TODO(), cm)
					if err != nil {
						reqLogger.Error(err, "creation of configmap failed")
						return err
					}
				} else {
					reqLogger.Error(err, "unable the get the configmap")
					return err
				}
			}
		case "Secret":
			sec := &corev1.Secret{
				ObjectMeta: objectMeta,
				Data:       make(map[string][]byte),
			}
			for k, v := range d.Data {
				sec.Data[k] = []byte(v)
			}
			found := &corev1.Secret{ObjectMeta: objectMeta}
			err := r.client.Get(context.TODO(), objectKey, found)
			if err != nil {
				if errors.IsNotFound(err) {
					reqLogger.Info(fmt.Sprintf("creating secret: %s", sec.Name))
					err = r.client.Create(context.TODO(), sec)
					if err != nil {
						reqLogger.Error(err, "creation of secret failed")
						return err
					}
				} else {
					reqLogger.Error(err, "unable to get the secret")
					return err
				}
			}
		case "StorageClass":
			var sc *storagev1.StorageClass
			if d.Name == cephFsStorageClassName {
				// Setting the fsname for cephfs StorageClass
				sc = scs[0]
			} else if d.Name == cephRbdStorageClassName {
				// Setting the PoolName for RBD StorageClass
				sc = scs[1]
			} else if d.Name == cephRgwStorageClassName {
				// Set the external rgw endpoint variable for later use on the Noobaa CR (as a label)
				// Replace the colon with an underscore, otherwise the label will be invalid
				externalRgwEndpointReplaceColon := strings.Replace(d.Data[externalCephRgwEndpointKey], ":", "_", -1)
				externalRgwEndpoint = externalRgwEndpointReplaceColon

				// Setting the Endpoint for OBC StorageClass
				sc = scs[2]
			}
			for k, v := range d.Data {
				sc.Parameters[k] = v
			}
		}
	}
	// creating all the storageClasses once we set the values
	err = r.createStorageClasses(scs, reqLogger)
	if err != nil {
		return err
	}
	instance.Status.ExternalSecretFound = true
	return nil
}
