// blackbox_exporter_test.go
package storagecluster

import (
	"context"
	"fmt"
	"testing"

	"github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned/scheme"
	securityv1 "github.com/openshift/api/security/v1"
	fakeclientset "github.com/openshift/client-go/security/clientset/versioned/fake"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func init() {
	// Add required schemes
	_ = clientgoscheme.AddToScheme(scheme.Scheme)
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

	securityClient := fakeclientset.NewClientset()

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

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = monitoringv1.AddToScheme(scheme)
	_ = ocsv1.AddToScheme(scheme)
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

	err := reconciler.createBlackboxDeployment(context.TODO(), instance, map[string]string{})
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
	assert.Equal(t, "/probe", probe.Spec.ProberSpec.Path)
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
				URL:  fmt.Sprintf("%s.%s.svc:%d", blackboxExporterName, instance.Namespace, blackboxPortNumber),
				Path: "/probe",
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

// TestBuildIPRegex verifies that buildIPRegex generates the correct regex string
func TestBuildIPRegex(t *testing.T) {
	tests := []struct {
		name     string
		ips      []string
		expected string
	}{
		{
			name:     "empty list returns never-match regex",
			ips:      []string{},
			expected: "$^",
		},
		{
			name:     "single IPv4 address",
			ips:      []string{"10.0.0.1"},
			expected: "^(10\\.0\\.0\\.1)$",
		},
		{
			name:     "multiple IPv4 addresses",
			ips:      []string{"10.128.2.82", "10.129.2.60", "10.131.0.49"},
			expected: "^(10\\.128\\.2\\.82|10\\.129\\.2\\.60|10\\.131\\.0\\.49)$",
		},
		{
			name:     "IPv6 standard format",
			ips:      []string{"2001:db8::1"},
			expected: "^(2001:db8::1)$",
		},
		{
			name:     "IPv6 loopback",
			ips:      []string{"::1"},
			expected: "^(::1)$",
		},
		{
			name:     "multiple IPv6 addresses",
			ips:      []string{"fe80::1", "2001:db8::1", "::1"},
			expected: "^(fe80::1|2001:db8::1|::1)$",
		},
		{
			name:     "mixed IPv4 and IPv6",
			ips:      []string{"10.0.0.1", "2001:db8::1"},
			expected: "^(10\\.0\\.0\\.1|2001:db8::1)$",
		},
		{
			name:     "IPv4 with various octets",
			ips:      []string{"192.168.1.1", "10.255.255.255", "0.0.0.0"},
			expected: "^(192\\.168\\.1\\.1|10\\.255\\.255\\.255|0\\.0\\.0\\.0)$",
		},
		{
			name:     "IPv6 with zone ID",
			ips:      []string{"fe80::1%eth0"},
			expected: "^(fe80::1%eth0)$",
		},
		{
			name:     "single digit octets",
			ips:      []string{"1.2.3.4"},
			expected: "^(1\\.2\\.3\\.4)$",
		},
		{
			name:     "large IPv6 address",
			ips:      []string{"2001:0db8:85a3:0000:0000:8a2e:0370:7334"},
			expected: "^(2001:0db8:85a3:0000:0000:8a2e:0370:7334)$",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildIPRegex(tt.ips)
			if got != tt.expected {
				t.Errorf("buildIPRegex(%q) = %q, want %q", tt.ips, got, tt.expected)
			}
		})
	}
}

// TestBuildMultusAnnotation verifies the annotation string generation for Multus networks
func TestBuildMultusAnnotation(t *testing.T) {
	tests := []struct {
		name      string
		networks  []NetworkAttachment
		namespace string
		expected  string
	}{
		{
			name:      "empty networks",
			networks:  []NetworkAttachment{},
			namespace: "openshift-storage",
			expected:  "",
		},
		{
			name: "single network, same namespace",
			networks: []NetworkAttachment{
				{Name: "macvlan1", Namespace: "openshift-storage"},
			},
			namespace: "openshift-storage",
			expected:  "macvlan1",
		},
		{
			name: "single network, different namespace",
			networks: []NetworkAttachment{
				{Name: "macvlan1", Namespace: "other-ns"},
			},
			namespace: "openshift-storage",
			expected:  "other-ns/macvlan1",
		},
		{
			name: "multiple networks",
			networks: []NetworkAttachment{
				{Name: "macvlan1", Namespace: "openshift-storage"},
				{Name: "macvlan2", Namespace: "other-ns"},
			},
			namespace: "openshift-storage",
			expected:  `[{"name":"macvlan1","namespace":"openshift-storage"},{"name":"macvlan2","namespace":"other-ns"}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildMultusAnnotation(tt.networks, tt.namespace)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestGetCephDaemonPodIPsFromNetworks verifies IP extraction logic for Multus and default SDN
func TestGetCephDaemonPodIPsFromNetworks(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = ocsv1.AddToScheme(scheme)

	tests := []struct {
		name        string
		pods        []corev1.Pod
		networks    []NetworkAttachment
		expectedIPs []string
	}{
		{
			name: "empty networks slice uses default PodIP",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "openshift-storage", Labels: map[string]string{"ceph_daemon_type": "osd"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.1"},
				},
			},
			networks:    []NetworkAttachment{},
			expectedIPs: []string{"10.0.0.1"},
		},
		{
			name: "networks specified, matching network-status annotation extracts Multus IPs",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "openshift-storage",
						Labels:    map[string]string{"ceph_daemon_type": "osd"},
						Annotations: map[string]string{
							"k8s.v1.cni.cncf.io/network-status": `[{"name": "openshift-storage/macvlan1", "interface": "eth1", "ips": ["192.168.1.10"]}]`,
						},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.2"},
				},
			},
			networks: []NetworkAttachment{
				{Name: "macvlan1", Namespace: "openshift-storage"},
			},
			expectedIPs: []string{"192.168.1.10"},
		},
		{
			name: "networks specified, but no network-status annotation falls back to PodIP",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod3",
						Namespace: "openshift-storage",
						Labels:    map[string]string{"ceph_daemon_type": "osd"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.3"},
				},
			},
			networks: []NetworkAttachment{
				{Name: "macvlan1", Namespace: "openshift-storage"},
			},
			expectedIPs: []string{"10.0.0.3"},
		},
		{
			name: "networks specified, network-status exists but no matching network returns empty",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod4",
						Namespace: "openshift-storage",
						Labels:    map[string]string{"ceph_daemon_type": "osd"},
						Annotations: map[string]string{
							"k8s.v1.cni.cncf.io/network-status": `[{"name": "openshift-storage/other-net", "interface": "eth1", "ips": ["192.168.2.10"]}]`,
						},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.4"},
				},
			},
			networks: []NetworkAttachment{
				{Name: "macvlan1", Namespace: "openshift-storage"},
			},
			expectedIPs: []string{},
		},
		{
			name: "non-running pods are ignored",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pod5", Namespace: "openshift-storage", Labels: map[string]string{"ceph_daemon_type": "osd"}},
					Status:     corev1.PodStatus{Phase: corev1.PodPending, PodIP: "10.0.0.5"},
				},
			},
			networks:    []NetworkAttachment{},
			expectedIPs: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objs []client.Object
			for i := range tt.pods {
				objs = append(objs, &tt.pods[i])
			}

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
			reconciler := &StorageClusterReconciler{
				Client: fakeClient,
				Log:    log,
			}

			ips, err := reconciler.getCephDaemonPodIPsFromNetworks(context.TODO(), "openshift-storage", "osd", tt.networks)
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.expectedIPs, ips)
		})
	}
}
