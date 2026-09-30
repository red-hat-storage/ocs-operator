package storagecluster

import (
	"strconv"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newExternalCephCluster(sc *ocsv1.StorageCluster, monitoringIP, monitoringPort string) *rookCephv1.CephCluster {
	labels := map[string]string{
		"app": sc.Name,
	}

	maxLogSize := resource.MustParse("500Mi")

	logCollector := rookCephv1.LogCollectorSpec{
		Enabled:     true,
		Periodicity: "daily",
		MaxLogSize:  &maxLogSize,
	}

	var monitoringSpec = rookCephv1.MonitoringSpec{Enabled: false}

	if monitoringIP != "" {
		monitoringSpec.Enabled = true
		// replace any comma with space and collect all the non-empty items
		monIPArr := parseMonitoringIPs(monitoringIP)
		monitoringSpec.ExternalMgrEndpoints = make([]corev1.EndpointAddress, len(monIPArr))
		for idx, eachMonIP := range monIPArr {
			monitoringSpec.ExternalMgrEndpoints[idx].IP = eachMonIP
		}
		if monitoringPort != "" {
			if uint16Val, err := strconv.ParseUint(monitoringPort, 10, 16); err == nil {
				monitoringSpec.ExternalMgrPrometheusPort = uint16(uint16Val)
			}
		}
	}

	externalCephCluster := &rookCephv1.CephCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.GenerateNameForCephCluster(sc),
			Namespace: sc.Namespace,
			Labels:    labels,
		},
		Spec: rookCephv1.ClusterSpec{
			External: rookCephv1.ExternalSpec{
				Enable: true,
			},
			CrashCollector: rookCephv1.CrashCollectorSpec{
				Disable: true,
			},
			DisruptionManagement: rookCephv1.DisruptionManagementSpec{
				ManagePodBudgets:               false,
				ManageMachineDisruptionBudgets: false,
			},
			Monitoring: monitoringSpec,
			Network:    getNetworkSpec(*sc),
			Labels: rookCephv1.LabelsSpec{
				rookCephv1.KeyMonitoring:   getCephClusterMonitoringLabels(*sc),
				rookCephv1.KeyCephExporter: getCephClusterMonitoringLabels(*sc),
			},
			Annotations:  util.GetRookCephDaemonSCCAnnotations(),
			LogCollector: logCollector,
			CSI: rookCephv1.CSIDriverSpec{
				ReadAffinity: util.GetReadAffinityOptions(sc),
				CephFS: rookCephv1.CSICephFSSpec{
					KernelMountOptions: util.GetCephFSKernelMountOptions(sc),
				},
			},
		},
	}

	return externalCephCluster
}
