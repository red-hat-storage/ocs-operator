package ocsinitialization

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	rookCephMetrics   = "rook-ceph-metrics"
	rookCephMonitor   = "rook-ceph-monitor"
	rookCephMonitorSA = "rook-ceph-system"
	noobaaMetrics     = "noobaa-metrics"
)

func (r *ReconcileOCSInitialization) ensureRBAC(initialData *ocsv1.OCSInitialization, reqLogger logr.Logger) error {

	if err := r.ensureRoles(initialData, reqLogger); err != nil {
		return err
	}

	if err := r.ensureRoleBindings(initialData, reqLogger); err != nil {
		return err
	}

	return nil
}

func (r *ReconcileOCSInitialization) ensureRoles(initialData *ocsv1.OCSInitialization, reqLogger logr.Logger) error {
	for _, role := range getAllRoles(initialData.Namespace) {
		found := &rbacv1.Role{}
		namespacedName := types.NamespacedName{
			Name:      role.ObjectMeta.Name,
			Namespace: initialData.Namespace,
		}
		err := r.client.Get(context.TODO(), namespacedName, found)
		if err != nil && errors.IsNotFound(err) {
			reqLogger.Info("Creating Role ", "role", role.ObjectMeta.Name)
			if err := r.client.Create(context.TODO(), role); err != nil {
				return fmt.Errorf("unable to create Role %+v: %v", role, err)
			}
		} else if err == nil {
			if !reflect.DeepEqual(role.Rules, found.Rules) {
				reqLogger.Info("Updating Role ", "role", role.ObjectMeta.Name)
				found.Rules = role.Rules
				if err := r.client.Update(context.TODO(), found); err != nil {
					return fmt.Errorf("unable to update Role %+v: %v", found, err)
				}
			}
		} else {
			return fmt.Errorf("unable to get Role %+v: %v", role, err)
		}
	}
	return nil
}

func (r *ReconcileOCSInitialization) ensureRoleBindings(initialData *ocsv1.OCSInitialization, reqLogger logr.Logger) error {
	for _, rb := range getAllRoleBindings(initialData.Namespace) {
		found := &rbacv1.RoleBinding{}
		namespacedName := types.NamespacedName{
			Name:      rb.ObjectMeta.Name,
			Namespace: initialData.Namespace,
		}
		err := r.client.Get(context.TODO(), namespacedName, found)
		if err != nil && errors.IsNotFound(err) {
			reqLogger.Info("Creating RoleBinding ", "rolebinding", rb.ObjectMeta.Name)
			if err := r.client.Create(context.TODO(), rb); err != nil {
				return fmt.Errorf("unable to create RoleBinding %+v: %v", rb, err)
			}
		} else if err == nil {
			if !reflect.DeepEqual(rb.RoleRef, found.RoleRef) || !reflect.DeepEqual(rb.Subjects, found.Subjects) {
				reqLogger.Info("Updating RoleBinding ", "rb", rb.ObjectMeta.Name)
				found.Subjects = rb.Subjects
				found.RoleRef = rb.RoleRef
				if err := r.client.Update(context.TODO(), found); err != nil {
					return fmt.Errorf("unable to update RoleBinding %+v: %v", found, err)
				}
			}
		} else {
			return fmt.Errorf("unable to get RoleBinding %+v: %v", rb, err)
		}
	}
	return nil
}

func getAllRoles(namespace string) []*rbacv1.Role {
	return []*rbacv1.Role{
		getMetricsRole(rookCephMetrics, namespace),
		getMetricsRole(noobaaMetrics, namespace),

		getMonitorRole(rookCephMonitor, namespace),
	}
}

func getAllRoleBindings(namespace string) []*rbacv1.RoleBinding {
	return []*rbacv1.RoleBinding{
		getMetricsRoleBinding(rookCephMetrics, namespace),
		getMetricsRoleBinding(noobaaMetrics, namespace),

		getMonitorRoleBinding(rookCephMonitor, namespace, rookCephMonitorSA),
	}
}

func getMetricsRole(name, namespace string) *rbacv1.Role {
	role := blankRole(name, namespace)
	role.Rules = []rbacv1.PolicyRule{
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"services",
				"endpoints",
				"pods",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
		},
	}

	return role
}

func getMonitorRole(name, namespace string) *rbacv1.Role {
	role := blankRole(name, namespace)
	role.Rules = []rbacv1.PolicyRule{
		{
			APIGroups: []string{
				"monitoring.coreos.com",
			},
			Resources: []string{
				"*",
			},
			Verbs: []string{
				"*",
			},
		},
	}

	return role
}

func blankRole(name, namespace string) *rbacv1.Role {
	return &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "Role",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func getMetricsRoleBinding(name, namespace string) *rbacv1.RoleBinding {
	rb := blankRoleBinding(name, namespace)
	rb.Subjects = []rbacv1.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      "prometheus-k8s",
			Namespace: "openshift-monitoring",
		},
	}

	return rb
}

func getMonitorRoleBinding(name, namespace, serviceaccount string) *rbacv1.RoleBinding {
	rb := blankRoleBinding(name, namespace)
	rb.Subjects = []rbacv1.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      serviceaccount,
			Namespace: namespace,
		},
	}

	return rb
}

func blankRoleBinding(name, namespace string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "Role",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     name,
		},
	}
}
