package storagecluster

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
)

// StatusUpdate updates verbose log to a shorter and concise log
func (r *ReconcileStorageCluster) StatusUpdate(obj runtime.Object) error {
	err := r.client.Status().Update(context.TODO(), obj)
	if errors.IsConflict(err) {
		return fmt.Errorf("the object has been updated")
	}
	return err
}
