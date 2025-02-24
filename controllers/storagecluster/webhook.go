package storagecluster

import (
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	admrv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	OcsMutatingWebhookConfigName = "ocs-operator.ocs.openshift.io"
	WebhookServiceTargetPort     = 7443
	webhookServicePort           = 443
	storageClassWebhookName      = "storageclass.ocs.openshift.io"
	mutateStorageClassEndpoint   = "/mutate-storageclass"
	// should be the name from rbac/webhook-service.yaml
	webhookServiceName = "ocs-operator-webhook-server"
)

type storageClassWebhook struct{}

var _ resourceManager = &storageClassWebhook{}

// should match the spec at rbac/webhook-service.yaml
var webhookService = corev1.Service{
	Spec: corev1.ServiceSpec{
		Ports: []corev1.ServicePort{
			{
				Name:       "ocs-operator-webhook",
				Port:       webhookServicePort,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt32(WebhookServiceTargetPort),
			},
		},
		Selector: map[string]string{
			"name": "ocs-operator",
		},
		Type: corev1.ServiceTypeClusterIP,
	},
}

var storageClassMutatingWebhook = admrv1.MutatingWebhook{
	ClientConfig: admrv1.WebhookClientConfig{
		Service: &admrv1.ServiceReference{
			Name: webhookServiceName,
			Path: ptr.To(mutateStorageClassEndpoint),
			Port: ptr.To(int32(webhookServicePort)),
		},
	},
	Rules: []admrv1.RuleWithOperations{
		{
			Rule: admrv1.Rule{
				APIGroups:   []string{"storage.k8s.io"},
				APIVersions: []string{"v1"},
				Resources:   []string{"storageclasses"},
				Scope:       ptr.To(admrv1.ClusterScope),
			},
			Operations: []admrv1.OperationType{admrv1.Create},
		},
	},
	FailurePolicy:           ptr.To(admrv1.Fail),
	SideEffects:             ptr.To(admrv1.SideEffectClassNone),
	TimeoutSeconds:          ptr.To(int32(30)),
	AdmissionReviewVersions: []string{"v1"},
	MatchConditions: []admrv1.MatchCondition{
		{
			Name: "onlyOcsProvisioners",
			Expression: fmt.Sprintf(
				"request.object.provisioner in ['%s', '%s', '%s']",
				util.RbdDriverName,
				util.CephFSDriverName,
				util.NfsDriverName,
			),
		},
	},
}

func (s *storageClassWebhook) ensureCreated(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (ctrl.Result, error) {
	svc := &corev1.Service{}
	svc.Name = storageClassWebhookName
	svc.Namespace = r.OperatorNamespace
	if _, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, svc, func() error {
		if err := controllerutil.SetControllerReference(storageCluster, svc, r.Scheme); err != nil {
			r.Log.Error(err, "failed to own resources", "owner", storageCluster.Name, "dependent", svc.Name)
			return err
		}
		util.AddAnnotation(svc, "service.beta.openshift.io/serving-cert-secret-name", "ocs-operator-webhook-cert-secret")
		webhookService.Spec.DeepCopyInto(&svc.Spec)
		return nil
	}); err != nil {
		return ctrl.Result{}, err
	}

	whConfig := &admrv1.MutatingWebhookConfiguration{}
	whConfig.Name = OcsMutatingWebhookConfigName
	if _, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, whConfig, func() error {
		// openshift fills in the ca on finding this annotation
		whConfig.Annotations = map[string]string{
			"service.beta.openshift.io/inject-cabundle": "true",
		}

		var caBundle []byte
		if len(whConfig.Webhooks) == 0 {
			whConfig.Webhooks = make([]admrv1.MutatingWebhook, 1)
		} else {
			// do not mutate CA bundle that was injected by openshift
			caBundle = whConfig.Webhooks[0].ClientConfig.CABundle
		}

		// webhook desired state
		var wh *admrv1.MutatingWebhook = &whConfig.Webhooks[0]
		storageClassMutatingWebhook.DeepCopyInto(wh)
		wh.Name = storageClassWebhookName
		// preserve the existing (injected) CA bundle if any
		wh.ClientConfig.CABundle = caBundle
		// send request to the service running in own namespace
		wh.ClientConfig.Service.Namespace = r.OperatorNamespace

		return nil
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (s *storageClassWebhook) ensureDeleted(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (ctrl.Result, error) {
	whConfig := &admrv1.MutatingWebhookConfiguration{}
	whConfig.Name = OcsMutatingWebhookConfigName
	if err := r.Delete(r.ctx, whConfig); client.IgnoreNotFound(err) != nil {
		r.Log.Error(err, "failed to delete mutating webhook configuration", "name", whConfig.Name)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}
