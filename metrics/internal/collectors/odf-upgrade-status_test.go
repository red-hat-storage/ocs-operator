package collectors

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestOdfUpgradeStatusCollect(t *testing.T) {
	collector := &OdfUpgradeStatusCollector{
		DependenciesUpgradeBlocked: prometheus.NewDesc(
			prometheus.BuildFQName("odf", "operator", "dependencies_upgrade_blocked"),
			"test",
			[]string{"namespace", "last_accepted_csv", "last_accepted_csv_build_date"},
			nil,
		),
		operatorNamespace: "openshift-storage",
	}

	tests := []struct {
		name          string
		configmap     *corev1.ConfigMap
		expectMetric  bool
		expectBlocked float64
	}{
		{
			name:         "configmap not found",
			configmap:    nil,
			expectMetric: false,
		},
		{
			name: "upgrade not blocked",
			configmap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: odfOperatorInternalConfigMapName, Namespace: "openshift-storage"},
				Data: map[string]string{
					lastAcceptedCSVKey:            "odf-operator.v4.21.0",
					lastAcceptedCSVBuildDateKey:   "2026-04-22T09:54:14Z",
					dependenciesUpgradeBlockedKey: "false",
				},
			},
			expectMetric:  true,
			expectBlocked: 0,
		},
		{
			name: "upgrade blocked",
			configmap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: odfOperatorInternalConfigMapName, Namespace: "openshift-storage"},
				Data: map[string]string{
					lastAcceptedCSVKey:            "odf-operator.v4.21.0",
					lastAcceptedCSVBuildDateKey:   "2026-04-22T09:54:14Z",
					dependenciesUpgradeBlockedKey: "true",
				},
			},
			expectMetric:  true,
			expectBlocked: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := k8sfake.NewSimpleClientset()
			if test.configmap != nil {
				_, err := client.CoreV1().ConfigMaps("openshift-storage").Create(context.TODO(), test.configmap, metav1.CreateOptions{})
				assert.NoError(t, err)
			}
			collector.k8sclient = client

			ch := make(chan prometheus.Metric, 1)
			collector.Collect(ch)
			close(ch)

			metrics := make([]prometheus.Metric, 0)
			for m := range ch {
				metrics = append(metrics, m)
			}

			if !test.expectMetric {
				assert.Empty(t, metrics)
				return
			}

			assert.Len(t, metrics, 1)
			dtoMetric := &dto.Metric{}
			assert.NoError(t, metrics[0].Write(dtoMetric))
			assert.Equal(t, test.expectBlocked, *dtoMetric.GetGauge().Value)

			var lastAcceptedCSV string
			for _, label := range dtoMetric.GetLabel() {
				if label.GetName() == "last_accepted_csv" {
					lastAcceptedCSV = label.GetValue()
				}
			}
			assert.Equal(t, "odf-operator.v4.21.0", lastAcceptedCSV)
		})
	}
}
