package storagecluster

import (
	"context"
	"fmt"
	"testing"

	securityv1 "github.com/openshift/api/security/v1"
	fakeclientset "github.com/openshift/client-go/security/clientset/versioned/fake"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func init() {
	// Add required schemes
	_ = monitoringv1.AddToScheme(scheme.Scheme)
	_ = securityv1.Install(scheme.Scheme)
	_ = ocsv1.AddToScheme(scheme.Scheme)
}

func TestCreateBlackboxServiceAccount(t *testing.T) {
	scheme := scheme.Scheme
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &StorageClusterReconciler{
		Client: client,
		Log:    log,
	}

	instance := &ocsv1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-storagecluster",
			Namespace: "openshift-storage",
		},
	}

	err := reconciler.createBlackboxServiceAccount(context.TODO(), instance)
	require.NoError(t, err)

	sa := &corev1.ServiceAccount{}
	err = client.Get(context.TODO(), types.NamespacedName{
		Namespace: "openshift-storage",
		Name:      blackboxServiceAccount,
	}, sa)
	require.NoError(t, err)
	assert.Equal(t, blackboxExporterLabels, sa.Labels)
	assert.Len(t, sa.OwnerReferences, 1)
}

func TestCreateBlackboxSCC(t *testing.T) {
	kubeClient := fake.NewClientBuilder().Build()

	securityClient := fakeclientset.NewSimpleClientset()

	reconciler := &StorageClusterReconciler{
		Client:         kubeClient,
		SecurityClient: securityClient.SecurityV1(),
		Log:            log,
	}

	instance := &ocsv1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-storagecluster",
			Namespace: "openshift-storage",
		},
	}

	err := reconciler.createBlackboxSCC(context.TODO(), instance)
	require.NoError(t, err)

	// Retrieve the created SCC from the fake client
	scc, err := securityClient.SecurityV1().SecurityContextConstraints().Get(context.TODO(), blackboxSCCName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, blackboxSCCName, scc.Name)
}

func TestCreateBlackboxConfigMap(t *testing.T) {
	scheme := scheme.Scheme
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &StorageClusterReconciler{
		Client: client,
		Log:    log,
		Scheme: scheme,
	}

	instance := &ocsv1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-storagecluster",
			Namespace: "openshift-storage",
		},
	}

	err := reconciler.createBlackboxConfigMap(context.TODO(), instance)
	require.NoError(t, err)

	cm := &corev1.ConfigMap{}
	err = client.Get(context.TODO(), types.NamespacedName{
		Namespace: "openshift-storage",
		Name:      blackboxConfigMapName,
	}, cm)
	require.NoError(t, err)
	assert.Contains(t, cm.Data["config.yml"], "icmp_internal")
	assert.Equal(t, blackboxExporterLabels, cm.Labels)
}

func TestCreateBlackboxDeployment(t *testing.T) {
	scheme := scheme.Scheme
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &StorageClusterReconciler{
		Client: client,
		Log:    log,
		Scheme: scheme,
		images: ImageMap{
			BlackboxExporter: "quay.io/prometheus/blackbox-exporter:v0.25.0",
		},
	}

	instance := &ocsv1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-storagecluster",
			Namespace: "openshift-storage",
		},
	}

	err := reconciler.createBlackboxDeployment(context.TODO(), instance)
	require.NoError(t, err)

	deployment := &appsv1.Deployment{}
	err = client.Get(context.TODO(), types.NamespacedName{
		Namespace: "openshift-storage",
		Name:      blackboxExporterName,
	}, deployment)
	require.NoError(t, err)

	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Equal(t, "quay.io/prometheus/blackbox-exporter:v0.25.0", container.Image)
	assert.Equal(t, "NET_RAW", string(container.SecurityContext.Capabilities.Add[0]))
	assert.True(t, *container.SecurityContext.RunAsNonRoot)
	assert.Equal(t, "0 2147483647", deployment.Spec.Template.Spec.SecurityContext.Sysctls[0].Value)
}

func TestCreateBlackboxService(t *testing.T) {
	scheme := scheme.Scheme
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &StorageClusterReconciler{
		Client: client,
		Log:    log,
		Scheme: scheme,
	}

	instance := &ocsv1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-storagecluster",
			Namespace: "openshift-storage",
		},
	}

	err := reconciler.createBlackboxService(context.TODO(), instance)
	require.NoError(t, err)

	svc := &corev1.Service{}
	err = client.Get(context.TODO(), types.NamespacedName{
		Namespace: "openshift-storage",
		Name:      blackboxExporterName,
	}, svc)
	require.NoError(t, err)
	assert.Equal(t, int32(9115), svc.Spec.Ports[0].Port)
	assert.Equal(t, blackboxExporterLabels, svc.Spec.Selector)
}

func TestCreateBlackboxProbe(t *testing.T) {
	instance := &ocsv1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-storagecluster",
			Namespace: "openshift-storage",
		},
	}
	nodeIPs := []string{"10.0.1.10"}

	probe := getDesiredProbe(instance, nodeIPs)

	assert.Equal(t, "odf-blackbox-exporter.openshift-storage.svc:9115", probe.Spec.ProberSpec.URL)
	require.NotNil(t, probe.Spec.Targets.StaticConfig)
	assert.Contains(t, probe.Spec.Targets.StaticConfig.Targets, "10.0.1.10")
	assert.Equal(t, "icmp_internal", probe.Spec.Module)
}

func getDesiredProbe(instance *ocsv1.StorageCluster, nodeIPs []string) *monitoringv1.Probe {
	return &monitoringv1.Probe{
		ObjectMeta: metav1.ObjectMeta{
			Name:      blackboxExporterName,
			Namespace: instance.Namespace,
			Labels:    blackboxExporterLabels,
		},
		Spec: monitoringv1.ProbeSpec{
			ProberSpec: monitoringv1.ProberSpec{
				URL: fmt.Sprintf("%s.%s.svc:%d", blackboxExporterName, instance.Namespace, blackboxPortNumber),
			},
			Module: "icmp_internal",
			Targets: monitoringv1.ProbeTargets{
				StaticConfig: &monitoringv1.ProbeTargetStaticConfig{
					Targets: nodeIPs,
					Labels:  map[string]string{"job": blackboxExporterName},
				},
			},
			Interval:      blackboxScrapeInterval,
			ScrapeTimeout: "5s",
		},
	}
}

func TestGetNodeIPs(t *testing.T) {
	scheme := scheme.Scheme
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "worker-1",
			Labels: map[string]string{defaults.NodeAffinityKey: ""},
		},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "10.0.1.10"},
			},
		},
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()
	reconciler := &StorageClusterReconciler{Client: client}

	ips, err := reconciler.getNodeIPs(context.TODO())
	require.NoError(t, err)
	assert.Equal(t, []string{"10.0.1.10"}, ips)
}
