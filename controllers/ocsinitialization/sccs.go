package ocsinitialization

import (
	"context"
	"fmt"

	cephcsi "github.com/ceph/ceph-csi/api/deploy/ocp"
	secv1 "github.com/openshift/api/security/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
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
		newNooBaaSCC(namespace),
		newNooBaaEndpointSCC(namespace),
		newMetricsExporterSCC(namespace),
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

func newNooBaaSCC(namespace string) *secv1.SecurityContextConstraints {
	scc := blankSCC()

	scc.Name = "noobaa"
	allowPrivilegeEscalation := true
	scc.AllowHostDirVolumePlugin = false
	scc.AllowHostIPC = false
	scc.AllowHostNetwork = false
	scc.AllowHostPID = false
	scc.AllowPrivilegeEscalation = &allowPrivilegeEscalation
	scc.AllowPrivilegedContainer = false
	scc.ReadOnlyRootFilesystem = false
	scc.AllowedCapabilities = []corev1.Capability{}
	scc.DefaultAddCapabilities = []corev1.Capability{}
	scc.RequiredDropCapabilities = []corev1.Capability{
		"KILL",
		"MKNOD",
		"SETUID",
		"SETGID",
	}
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
		fmt.Sprintf("system:serviceaccount:%s:noobaa", namespace),
	}

	return scc
}

func newNooBaaEndpointSCC(namespace string) *secv1.SecurityContextConstraints {
	scc := blankSCC()

	scc.Name = "noobaa-endpoint"
	allowPrivilegeEscalation := true
	scc.AllowHostDirVolumePlugin = false
	scc.AllowHostIPC = false
	scc.AllowHostNetwork = false
	scc.AllowHostPID = false
	scc.AllowPrivilegeEscalation = &allowPrivilegeEscalation
	scc.AllowPrivilegedContainer = false
	scc.ReadOnlyRootFilesystem = false
	scc.AllowedCapabilities = []corev1.Capability{
		"SETUID",
		"SETGID",
	}
	scc.DefaultAddCapabilities = []corev1.Capability{}
	scc.RequiredDropCapabilities = []corev1.Capability{
		"KILL",
		"MKNOD",
	}
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
		fmt.Sprintf("system:serviceaccount:%s:noobaa-endpoint", namespace),
	}

	return scc
}

func newMetricsExporterSCC(namespace string) *secv1.SecurityContextConstraints {
	scc := blankSCC()
	scc.Name = "ocs-metrics-exporter"
	scc.AllowHostDirVolumePlugin = false
	scc.AllowHostIPC = false
	scc.AllowHostNetwork = false
	scc.AllowHostPID = false
	scc.AllowHostPorts = false
	scc.AllowPrivilegedContainer = true
	scc.ReadOnlyRootFilesystem = false
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
		secv1.FSTypeConfigMap,
		secv1.FSTypeDownwardAPI,
		secv1.FSTypeEmptyDir,
		secv1.FSTypeHostPath,
		secv1.FSProjected,
	}
	scc.Users = []string{
		fmt.Sprintf("system:serviceaccount:%s:ocs-metrics-exporter", namespace),
	}
	return scc
}
