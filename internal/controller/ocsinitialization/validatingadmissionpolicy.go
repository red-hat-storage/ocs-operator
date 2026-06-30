package ocsinitialization

import (
	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	storageClusterDeletePolicyName        = "storagecluster-delete-protection.ocs.openshift.io"
	storageClusterDeletePolicyBindingName = "storagecluster-delete-protection-binding.ocs.openshift.io"
)

// The deletion of StorageCluster must only be permitted when the annotation "uninstall.ocs.openshift.io/confirm-deletion: true" is set on the StorageCluster.
// This is to prevent accidental deletion of the StorageCluster and ensure that the user has explicitly confirmed the deletion.
func (r *OCSInitializationReconciler) reconcileValidatingAdmissionPolicy() error {
	vap := &admissionv1.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: storageClusterDeletePolicyName,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, vap, func() error {
		vap.Spec = admissionv1.ValidatingAdmissionPolicySpec{
			FailurePolicy: ptr.To(admissionv1.Fail),
			MatchConstraints: &admissionv1.MatchResources{
				ResourceRules: []admissionv1.NamedRuleWithOperations{
					{
						RuleWithOperations: admissionv1.RuleWithOperations{
							Operations: []admissionv1.OperationType{admissionv1.Delete},
							Rule: admissionv1.Rule{
								APIGroups:   []string{"ocs.openshift.io"},
								APIVersions: []string{"v1"},
								Resources:   []string{"storageclusters"},
							},
						},
					},
				},
			},
			Validations: []admissionv1.Validation{
				{
					Expression:        `oldObject.metadata.?annotations['uninstall.ocs.openshift.io/confirm-deletion'].orValue('') == 'true'`,
					MessageExpression: `"WARNING: StorageCluster deletion is IRREVERSIBLE and will permanently destroy all stored data. To confirm you accept this risk, run: kubectl annotate storagecluster " + oldObject.metadata.name + " uninstall.ocs.openshift.io/confirm-deletion=true -- then retry deletion."`,
					Reason:     ptr.To(metav1.StatusReasonForbidden),
				},
			},
		}
		return nil
	}); err != nil {
		return err
	}

	vapBinding := &admissionv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: storageClusterDeletePolicyBindingName,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, vapBinding, func() error {
		vapBinding.Spec = admissionv1.ValidatingAdmissionPolicyBindingSpec{
			PolicyName:        storageClusterDeletePolicyName,
			ValidationActions: []admissionv1.ValidationAction{admissionv1.Deny},
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}
