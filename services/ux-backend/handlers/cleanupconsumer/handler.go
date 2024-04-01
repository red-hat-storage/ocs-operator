package cleanupconsumer

import (
	"context"
	"fmt"
	"net/http"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func HandleMessage(w http.ResponseWriter, r *http.Request, cl client.Client) {
	switch r.Method {
	case "DELETE":
		consumerName := r.URL.Query().Get("consumerName")
		handleDelete(r.Context(), w, cl, consumerName)
	default:
		handleUnsupportedMethod(w, r)
	}
}

func handleDelete(ctx context.Context, w http.ResponseWriter, cl client.Client, consumerName string) {
	klog.Info("Delete method on /cleanup-consumer endpoint is invoked")
	w.Header().Set("Content-Type", handlers.ContentTypeTextPlain)
	if message, err := removeStorageConsumer(ctx, cl, consumerName); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", handlers.ContentTypeTextPlain)

		if _, err := w.Write([]byte("Failed to delete resources")); err != nil {
			klog.Errorf("failed write data to response writer, %v", err)
		}
	} else {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", handlers.ContentTypeTextPlain)

		if _, err = w.Write([]byte(message)); err != nil {
			klog.Errorf("failed write data to response writer: %v", err)
		}
	}

}

func handleUnsupportedMethod(w http.ResponseWriter, r *http.Request) {
	klog.Info("Only Delete method should be used to send data to this endpoint /cleanup-consumer")
	w.WriteHeader(http.StatusMethodNotAllowed)
	w.Header().Set("Content-Type", handlers.ContentTypeTextPlain)
	w.Header().Set("Allow", "DELETE")

	if _, err := w.Write([]byte(fmt.Sprintf("Unsupported method : %s", r.Method))); err != nil {
		klog.Errorf("failed to write data to response writer: %v", err)
	}
}

func removeStorageConsumer(ctx context.Context, cl client.Client, consumerName string) (string, error) {
	storageconsumer := &ocsv1alpha1.StorageConsumer{}
	storageconsumer.Name = consumerName
	storageconsumer.Namespace = handlers.GetPodNamespace()
	var err error
	err = cl.Get(ctx, client.ObjectKeyFromObject(storageconsumer), storageconsumer)
	if err != nil {
		return "", fmt.Errorf("failed to get storage consumer with name: %v", err)
	}

	if storageconsumer.Annotations == nil {
		storageconsumer.Annotations = make(map[string]string)
	}
	storageconsumer.Annotations[handlers.RookCephResourceForceDeleteAnnotation] = "true"
	// Clean up of resource is part of respective controller
	err = cl.Update(ctx, storageconsumer)
	if err != nil {
		return "", fmt.Errorf("failed to update storage consumer: %v", err)
	}

	// Trigger delete for cleaning up resources
	err = cl.Delete(ctx, storageconsumer)
	if err != nil {
		return "", fmt.Errorf("failed to delete storage consumer: %v", err)
	}

	return "Successfully deleted the storage consumer", nil
}
