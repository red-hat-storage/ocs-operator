package ocsinitialization

import (
	"context"
	"fmt"

	cephcsi "github.com/ceph/ceph-csi/api/deploy/ocp"
	secv1 "github.com/openshift/api/security/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

func (r *OCSInitializationReconciler) ensureSCCs(initialData *ocsv1.OCSInitialization) error {
	sccs := getAllSCCs(initialData.Namespace)
	for _, scc := range sccs {
		found, err := r.SecurityClient.SecurityContextConstraints().Get(context.TODO(), scc.Name, metav1.GetOptions{})

		if err != nil && errors.IsNotFound(err) {
			r.Log.Info("Creating SecurityContextConstraint.", "SecurityContextConstraint", klog.KRef(scc.Namespace, scc.Name))
			_, err := r.SecurityClient.SecurityContextConstraints().Create(context.TODO(), scc, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("unable to create SCC %+v: %v", scc, err)
			}
		} else if err == nil {
			scc.ObjectMeta = found.ObjectMeta
			r.Log.Info("Updating SecurityContextConstraint.", "SecurityContextConstraint", klog.KRef(scc.Namespace, scc.Name))
			_, err := r.SecurityClient.SecurityContextConstraints().Update(context.TODO(), scc, metav1.UpdateOptions{})
			if err != nil {
				r.Log.Error(err, "Unable to update SecurityContextConstraint.", "SecurityContextConstraint", klog.KRef(scc.Namespace, scc.Name))
				return fmt.Errorf("unable to update SCC %+v: %v", scc, err)
			}
		} else {
			r.Log.Error(err, "Something went wrong when checking for SecurityContextConstraint.", "SecurityContextConstraint", klog.KRef(scc.Namespace, scc.Name))
			return fmt.Errorf("something went wrong when checking for SCC %+v: %v", scc, err)
		}
	}

	return nil
}

func getAllSCCs(namespace string) []*secv1.SecurityContextConstraints {
	return []*secv1.SecurityContextConstraints{
		newRookCephSCC(namespace),
		newRookCephCSISCC(namespace),
	}
}

func newRookCephSCC(namespace string) *secv1.SecurityContextConstraints {
	scc := cephv1.NewSecurityContextConstraints("rook-ceph", namespace)
	// host networking could still be enabled in the cluster for prototyping
	scc.AllowHostNetwork = true
	scc.AllowHostPorts = true
	return scc
}

func newRookCephCSISCC(namespace string) *secv1.SecurityContextConstraints {
	rookValues := cephcsi.SecurityContextConstraintsValues{
		Namespace: namespace,
		Deployer:  "rook",
	}

	scc, _ := cephcsi.NewSecurityContextConstraints(rookValues)

	return scc
}
