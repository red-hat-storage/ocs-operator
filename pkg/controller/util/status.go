package util

import (
	"fmt"

	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	ocsv1alpha1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1alpha1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
)

// SetProgressingCondition sets the ProgressingCondition to True and other conditions to
// false or Unknown. Used when we are just starting to reconcile, and there are no existing
// conditions.
func SetProgressingCondition(conditions *[]conditionsv1.Condition, reason string, message string) {
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    ocsv1alpha1.ConditionReconcileComplete,
		Status:  corev1.ConditionUnknown,
		Reason:  reason,
		Message: message,
	})
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    conditionsv1.ConditionAvailable,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    conditionsv1.ConditionProgressing,
		Status:  corev1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    conditionsv1.ConditionDegraded,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    conditionsv1.ConditionUpgradeable,
		Status:  corev1.ConditionUnknown,
		Reason:  reason,
		Message: message,
	})
}

// SetErrorCondition sets the ConditionReconcileComplete to False in case of any errors
// during the reconciliation process.
func SetErrorCondition(conditions *[]conditionsv1.Condition, reason string, message string) {
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    ocsv1alpha1.ConditionReconcileComplete,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
}

// SetCompleteCondition sets the ConditionReconcileComplete to True and other Conditions
// to indicate that the reconciliation process has completed successfully.
func SetCompleteCondition(conditions *[]conditionsv1.Condition, reason string, message string) {
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    ocsv1alpha1.ConditionReconcileComplete,
		Status:  corev1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    conditionsv1.ConditionAvailable,
		Status:  corev1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    conditionsv1.ConditionProgressing,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    conditionsv1.ConditionDegraded,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    conditionsv1.ConditionUpgradeable,
		Status:  corev1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})
}

// MapCephClusterNegativeConditions maps the status states from CephCluster resource into ocs status conditions.
// This will only look for negative conditions: !Available, Degraded, Progressing
func MapCephClusterNegativeConditions(conditions *[]conditionsv1.Condition, found *rookCephv1.CephCluster) {
	switch found.Status.State {
	case rookCephv1.ClusterStateCreating:
		conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
			Type: conditionsv1.ConditionProgressing,
			Status: corev1.ConditionTrue,
			Reason: "ClusterStateCreating",
			Message: fmt.Sprintf("CephCluster is creating: %v", string(found.Status.Message)),
		})
		conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
			Type: conditionsv1.ConditionUpgradeable,
			Status: corev1.ConditionFalse,
			Reason: "ClusterStateCreating",
			Message: fmt.Sprintf("CephCluster is creating: %v", string(found.Status.Message)),
		})
	case rookCephv1.ClusterStateUpdating:
		conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
			Type: conditionsv1.ConditionProgressing,
			Status: corev1.ConditionTrue,
			Reason: "ClusterStateUpdating",
			Message: fmt.Sprintf("CephCluster is updating: %v", string(found.Status.Message)),
		})
		conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
			Type: conditionsv1.ConditionUpgradeable,
			Status: corev1.ConditionFalse,
			Reason: "ClusterStateUpdating",
			Message: fmt.Sprintf("CephCluster is updating: %v", string(found.Status.Message)),
		})
	case rookCephv1.ClusterStateError:
		conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
			Type: conditionsv1.ConditionAvailable,
			Status: corev1.ConditionFalse,
			Reason: "ClusterStateError",
			Message: fmt.Sprintf("CephCluster error: %v", string(found.Status.Message)),
		})
		conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
			Type: conditionsv1.ConditionDegraded,
			Status: corev1.ConditionTrue,
			Reason: "ClusterStateError",
			Message: fmt.Sprintf("CephCluster error: %v", string(found.Status.Message)),
		})
	}
}

// MapCephClusterNoConditions sets status conditions to progressing. Used when component operator isn't 
// reporting any status, and we have to assume progress.
func MapCephClusterNoConditions(conditions *[]conditionsv1.Condition, reason string, message string) {
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type: conditionsv1.ConditionAvailable,
		Status: corev1.ConditionFalse,
		Reason: reason,
		Message: message,
	})
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type: conditionsv1.ConditionProgressing,
		Status: corev1.ConditionTrue,
		Reason: reason,
		Message: message,
	})
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type: conditionsv1.ConditionUpgradeable,
		Status: corev1.ConditionFalse,
		Reason: reason,
		Message: message,
	})
}
