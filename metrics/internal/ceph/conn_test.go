package ceph

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestParseMonEndpoints(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single monitor",
			input: "a=10.0.0.1:6789",
			want:  "10.0.0.1:6789",
		},
		{
			name:  "three monitors",
			input: "a=10.0.0.1:6789,b=10.0.0.2:6789,c=10.0.0.3:6789",
			want:  "10.0.0.1:6789,10.0.0.2:6789,10.0.0.3:6789",
		},
		{
			name:  "whitespace around entries",
			input: " a=10.0.0.1:6789 , b=10.0.0.2:6789 ",
			want:  "10.0.0.1:6789,10.0.0.2:6789",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "no equals sign",
			input: "10.0.0.1:6789",
			want:  "",
		},
		{
			name:  "trailing comma",
			input: "a=10.0.0.1:6789,",
			want:  "10.0.0.1:6789",
		},
		{
			name:  "IPv6 address",
			input: "a=[::1]:6789,b=[::2]:6789",
			want:  "[::1]:6789,[::2]:6789",
		},
		{
			name:  "msgr2 port",
			input: "a=10.0.0.1:3300,b=10.0.0.2:3300",
			want:  "10.0.0.1:3300,10.0.0.2:3300",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMonEndpoints(tt.input)
			if got != tt.want {
				t.Errorf("parseMonEndpoints(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// mockK8s implements k8sReader for testing without a real cluster.
type mockK8s struct {
	secret    *corev1.Secret
	configmap *corev1.ConfigMap
	secretErr error
	cmErr     error
}

func (m *mockK8s) getSecret(_ context.Context, _, _ string) (*corev1.Secret, error) {
	if m.secretErr != nil {
		return nil, m.secretErr
	}
	return m.secret, nil
}

func (m *mockK8s) getConfigMap(_ context.Context, _, _ string) (*corev1.ConfigMap, error) {
	if m.cmErr != nil {
		return nil, m.cmErr
	}
	return m.configmap, nil
}

const testNs = "openshift-storage"

func validSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: util.OcsMetricsExporterCephClientName, Namespace: testNs},
		Data: map[string][]byte{
			"userID":  []byte("client.ocs-metrics"),
			"userKey": []byte("AQDsecretkey=="),
		},
	}
}

func validConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: monEndpointConfigMap, Namespace: testNs},
		Data:       map[string]string{"data": "a=10.0.0.1:6789,b=10.0.0.2:6789,c=10.0.0.3:6789"},
	}
}

func TestFetchCredentials(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockK8s
		wantUserID string
		wantKey    string
		wantMons   string
		wantErr    string
	}{
		{
			name:       "valid credentials and monitors",
			mock:       &mockK8s{secret: validSecret(), configmap: validConfigMap()},
			wantUserID: "client.ocs-metrics",
			wantKey:    "AQDsecretkey==",
			wantMons:   "10.0.0.1:6789,10.0.0.2:6789,10.0.0.3:6789",
		},
		{
			name:    "secret not found",
			mock:    &mockK8s{secretErr: fmt.Errorf("not found"), configmap: validConfigMap()},
			wantErr: "failed to get secret",
		},
		{
			name: "missing userID key",
			mock: &mockK8s{
				secret: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: util.OcsMetricsExporterCephClientName, Namespace: testNs},
					Data:       map[string][]byte{"userKey": []byte("key")},
				},
				configmap: validConfigMap(),
			},
			wantErr: "userID not found",
		},
		{
			name: "empty userKey",
			mock: &mockK8s{
				secret: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: util.OcsMetricsExporterCephClientName, Namespace: testNs},
					Data:       map[string][]byte{"userID": []byte("user"), "userKey": []byte("")},
				},
				configmap: validConfigMap(),
			},
			wantErr: "userKey not found",
		},
		{
			name:    "configmap not found",
			mock:    &mockK8s{secret: validSecret(), cmErr: fmt.Errorf("not found")},
			wantErr: "failed to get configmap",
		},
		{
			name: "missing data key in configmap",
			mock: &mockK8s{
				secret: validSecret(),
				configmap: &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: monEndpointConfigMap, Namespace: testNs},
					Data:       map[string]string{"maxMonId": "0"},
				},
			},
			wantErr: "data not found",
		},
		{
			name: "malformed mon endpoints",
			mock: &mockK8s{
				secret: validSecret(),
				configmap: &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: monEndpointConfigMap, Namespace: testNs},
					Data:       map[string]string{"data": "garbage-no-equals"},
				},
			},
			wantErr: "no monitor addresses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Conn{k8s: tt.mock, ns: testNs}

			creds, err := c.fetchCredentials()
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if creds.userID != tt.wantUserID {
				t.Errorf("userID = %q, want %q", creds.userID, tt.wantUserID)
			}
			if creds.userKey != tt.wantKey {
				t.Errorf("userKey = %q, want %q", creds.userKey, tt.wantKey)
			}
			if creds.monitors != tt.wantMons {
				t.Errorf("monitors = %q, want %q", creds.monitors, tt.wantMons)
			}
		})
	}
}

func TestReconnectAndCloseNilConn(t *testing.T) {
	c := &Conn{k8s: &mockK8s{}, ns: testNs}
	// Neither should panic on a nil connection.
	c.Reconnect()
	c.Close()
}
