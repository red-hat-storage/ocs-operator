package ocsinitialization

import (
	"fmt"

	"github.com/go-logr/logr"
	secv1 "github.com/openshift/api/security/v1"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (r *ReconcileOCSInitialization) ensureSCCs(initialData *ocsv1.OCSInitialization, reqLogger logr.Logger) error {
	sccs := getAllSCCs(initialData.Namespace)
	for _, scc := range sccs {
		found, err := r.secClient.SecurityContextConstraints().Get(scc.Name, metav1.GetOptions{})

		if err != nil && errors.IsNotFound(err) {
			reqLogger.Info(fmt.Sprintf("Creating %s SecurityContextConstraint", scc.Name))
			_, err := r.secClient.SecurityContextConstraints().Create(scc)
			if err != nil {
				return fmt.Errorf("unable to create SCC %+v: %v", scc, err)
			}
		} else if err == nil {
			scc.ObjectMeta = found.ObjectMeta
			reqLogger.Info(fmt.Sprintf("Updating %s SecurityContextConstraint", scc.Name))
			_, err := r.secClient.SecurityContextConstraints().Update(scc)
			if err != nil {
				return fmt.Errorf("unable to update SCC %+v: %v", scc, err)
			}
		} else {
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

func blankSCC() *secv1.SecurityContextConstraints {
	return &secv1.SecurityContextConstraints{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "security.openshift.io/v1",
			Kind:       "SecurityContextConstraints",
		},
	}
}

func newRookCephSCC(namespace string) *secv1.SecurityContextConstraints {
	scc := blankSCC()

	scc.Name = "rook-ceph"
	scc.AllowPrivilegedContainer = true
	scc.AllowHostNetwork = true
	scc.AllowHostDirVolumePlugin = true
	scc.AllowHostPorts = true
	scc.AllowHostPID = true
	scc.AllowHostIPC = true
	scc.ReadOnlyRootFilesystem = false
	scc.RequiredDropCapabilities = []corev1.Capability{}
	scc.DefaultAddCapabilities = []corev1.Capability{}
	scc.RunAsUser = secv1.RunAsUserStrategyOptions{
		Type: secv1.RunAsUserStrategyRunAsAny,
	}
	scc.SELinuxContext = secv1.SELinuxContextStrategyOptions{
		Type: secv1.SELinuxStrategyMustRunAs,
	}
	scc.FSGroup = secv1.FSGroupStrategyOptions{
		Type: secv1.FSGroupStrategyMustRunAs,
	}
	scc.SupplementalGroups = secv1.SupplementalGroupsStrategyOptions{
		Type: secv1.SupplementalGroupsStrategyRunAsAny,
	}
	scc.Volumes = []secv1.FSType{
		secv1.FSTypeConfigMap,
		secv1.FSTypeDownwardAPI,
		secv1.FSTypeEmptyDir,
		secv1.FSTypeHostPath,
		secv1.FSTypePersistentVolumeClaim,
		secv1.FSProjected,
		secv1.FSTypeSecret,
	}
	scc.Users = []string{
		fmt.Sprintf("system:serviceaccount:%s:rook-ceph-system", namespace),
		fmt.Sprintf("system:serviceaccount:%s:default", namespace),
		fmt.Sprintf("system:serviceaccount:%s:rook-ceph-mgr", namespace),
		fmt.Sprintf("system:serviceaccount:%s:rook-ceph-osd", namespace),
	}

	return scc
}

func newRookCephCSISCC(namespace string) *secv1.SecurityContextConstraints {
	scc := blankSCC()

	scc.Name = "rook-ceph-csi"
	scc.AllowPrivilegedContainer = true
	scc.AllowHostNetwork = true
	scc.AllowHostDirVolumePlugin = true
	scc.AllowedCapabilities = []corev1.Capability{
		secv1.AllowAllCapabilities,
	}
	scc.AllowHostPorts = true
	scc.AllowHostPID = true
	scc.AllowHostIPC = true
	scc.ReadOnlyRootFilesystem = false
	scc.RequiredDropCapabilities = []corev1.Capability{}
	scc.DefaultAddCapabilities = []corev1.Capability{}
	scc.RunAsUser = secv1.RunAsUserStrategyOptions{
		Type: secv1.RunAsUserStrategyRunAsAny,
	}
	scc.SELinuxContext = secv1.SELinuxContextStrategyOptions{
		Type: secv1.SELinuxStrategyRunAsAny,
	}
	scc.FSGroup = secv1.FSGroupStrategyOptions{
		Type: secv1.FSGroupStrategyRunAsAny,
	}
	scc.SupplementalGroups = secv1.SupplementalGroupsStrategyOptions{
		Type: secv1.SupplementalGroupsStrategyRunAsAny,
	}
	scc.Volumes = []secv1.FSType{
		secv1.FSTypeAll,
	}
	scc.Users = []string{
		fmt.Sprintf("system:serviceaccount:%s:rook-csi-rbd-plugin-sa", namespace),
		fmt.Sprintf("system:serviceaccount:%s:rook-csi-rbd-provisioner-sa", namespace),
		fmt.Sprintf("system:serviceaccount:%s:rook-csi-rbd-attacher-sa", namespace),
		fmt.Sprintf("system:serviceaccount:%s:rook-csi-cephfs-plugin-sa", namespace),
		fmt.Sprintf("system:serviceaccount:%s:rook-csi-cephfs-provisioner-sa", namespace),
	}

	return scc
}
