// blackbox_exporter_test.go
package storagecluster

import (
	"context"
	"testing"

	"github.com/go-jose/go-jose/v4/testutils/require"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

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
