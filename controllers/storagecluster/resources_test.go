package storagecluster

import (
	"testing"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetDaemonResources(t *testing.T) {
	testCases := []struct {
		name                         string
		daemonName                   string
		resourceProfile              string
		specifiedResources           map[string]corev1.ResourceRequirements
		expectedResourceRequirements corev1.ResourceRequirements
	}{
		{
			name:               "mgr with lean profile",
			daemonName:         "mgr",
			resourceProfile:    "lean",
			specifiedResources: map[string]corev1.ResourceRequirements{},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("0.5"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
			},
		},
		{
			name:               "mon with balanced profile (default)",
			daemonName:         "mon",
			resourceProfile:    "",
			specifiedResources: map[string]corev1.ResourceRequirements{},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
			},
		},
		{
			name:               "osd with balanced profile (explicit)",
			daemonName:         "osd",
			resourceProfile:    "balanced",
			specifiedResources: map[string]corev1.ResourceRequirements{},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("5Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("5Gi"),
				},
			},
		},
		{
			name:               "mds with performance profile",
			daemonName:         "mds",
			resourceProfile:    "performance",
			specifiedResources: map[string]corev1.ResourceRequirements{},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
			},
		},
		{
			name:               "rgw with case insensitive profile",
			daemonName:         "rgw",
			resourceProfile:    "LEAN",
			specifiedResources: map[string]corev1.ResourceRequirements{},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			},
		},
		{
			name:            "mgr with custom resources overriding profile",
			daemonName:      "mgr",
			resourceProfile: "lean",
			specifiedResources: map[string]corev1.ResourceRequirements{
				"mgr": {
					Requests: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("2"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("4"),
					},
				},
			},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("1Gi"), // from lean profile
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("2Gi"), // from lean profile
				},
			},
		},
		{
			name:            "mon with only memory limits specified",
			daemonName:      "mon",
			resourceProfile: "performance",
			specifiedResources: map[string]corev1.ResourceRequirements{
				"mon": {
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
			},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("1.5"), // from performance profile
					// No memory request because only memory limit was specified
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1.5"), // from performance profile
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
		},
		{
			name:            "rgw with only memory requests specified",
			daemonName:      "rgw",
			resourceProfile: "lean",
			specifiedResources: map[string]corev1.ResourceRequirements{
				"rgw": {
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
			},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"), // from lean profile
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("1"), // from lean profile
					// No memory limit because only memory request was specified
				},
			},
		},
		{
			name:            "mds with only CPU limits specified",
			daemonName:      "mds",
			resourceProfile: "performance",
			specifiedResources: map[string]corev1.ResourceRequirements{
				"mds": {
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("5"),
					},
				},
			},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					// No CPU request because only CPU limit was specified
					corev1.ResourceMemory: resource.MustParse("8Gi"), // from performance profile
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("5"),
					corev1.ResourceMemory: resource.MustParse("8Gi"), // from performance profile
				},
			},
		},
		{
			name:               "noobaa-core fallback to daemon resources",
			daemonName:         "noobaa-core",
			resourceProfile:    "lean", // not in lean profile, should fallback
			specifiedResources: map[string]corev1.ResourceRequirements{},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("999m"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("999m"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
		},
		{
			name:            "noobaa-db with CPU request only",
			daemonName:      "noobaa-db",
			resourceProfile: "performance", // not in performance profile, should fallback
			specifiedResources: map[string]corev1.ResourceRequirements{
				"noobaa-db": {
					Requests: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("1"),
					},
				},
			},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("4Gi"), // from daemon resources
				},
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("4Gi"), // from daemon resources
				},
			},
		},
		{
			name:            "noobaa-endpoint with memory limit only",
			daemonName:      "noobaa-endpoint",
			resourceProfile: "balanced", // not in balanced profile, should fallback
			specifiedResources: map[string]corev1.ResourceRequirements{
				"noobaa-endpoint": {
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("3Gi"),
					},
				},
			},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("999m"), // from daemon resources
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("999m"), // from daemon resources
					corev1.ResourceMemory: resource.MustParse("3Gi"),
				},
			},
		},
		{
			name:            "nfs with both CPU request and limit",
			daemonName:      "nfs",
			resourceProfile: "lean", // not in lean profile, should fallback
			specifiedResources: map[string]corev1.ResourceRequirements{
				"nfs": {
					Requests: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("2"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("4"),
					},
				},
			},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("8Gi"), // from daemon resources
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("8Gi"), // from daemon resources
				},
			},
		},
		{
			name:            "rbd-mirror with memory request only",
			daemonName:      "rbd-mirror",
			resourceProfile: "performance", // not in performance profile, should fallback
			specifiedResources: map[string]corev1.ResourceRequirements{
				"rbd-mirror": {
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
				},
			},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"), // from daemon resources
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("1"), // from daemon resources
				},
			},
		},
		{
			name:            "ocs-metrics-exporter with both memory request and limit",
			daemonName:      "ocs-metrics-exporter",
			resourceProfile: "balanced", // not in balanced profile, should fallback
			specifiedResources: map[string]corev1.ResourceRequirements{
				"ocs-metrics-exporter": {
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
			},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("250m"), // from daemon resources
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"), // from daemon resources
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
			},
		},
		{
			name:               "crashcollector default resources",
			daemonName:         "crashcollector",
			resourceProfile:    "lean", // not in lean profile, should fallback
			specifiedResources: map[string]corev1.ResourceRequirements{},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("50m"),
					corev1.ResourceMemory: resource.MustParse("50Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("100Mi"),
				},
			},
		},
		{
			name:            "logcollector with CPU limit only",
			daemonName:      "logcollector",
			resourceProfile: "performance", // not in performance profile, should fallback
			specifiedResources: map[string]corev1.ResourceRequirements{
				"logcollector": {
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("200m"),
					},
				},
			},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("50Mi"), // from daemon resources
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("200m"),
					corev1.ResourceMemory: resource.MustParse("250Mi"), // from daemon resources
				},
			},
		},
		{
			name:            "exporter with mixed CPU and memory specifications",
			daemonName:      "exporter",
			resourceProfile: "balanced", // not in balanced profile, should fallback
			specifiedResources: map[string]corev1.ResourceRequirements{
				"exporter": {
					Requests: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("0.1"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
				},
			},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("0.1"),
					// No memory request because CPU was specified, so no defaults for CPU resource type
				},
				Limits: corev1.ResourceList{
					// No CPU limit because only CPU request was specified
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
			},
		},
		{
			name:            "noobaa-db-vol with storage request override",
			daemonName:      "noobaa-db-vol",
			resourceProfile: "lean", // not in lean profile, should fallback
			specifiedResources: map[string]corev1.ResourceRequirements{
				"noobaa-db-vol": {
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("100Gi"),
					},
				},
			},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("100Gi"),
				},
				Limits: corev1.ResourceList{},
			},
		},
		{
			name:               "non-existent daemon returns empty resources",
			daemonName:         "non-existent",
			resourceProfile:    "lean",
			specifiedResources: map[string]corev1.ResourceRequirements{},
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{},
				Limits:   corev1.ResourceList{},
			},
		},
		{
			name:            "mon with empty resource requirements",
			daemonName:      "mon",
			resourceProfile: "balanced",
			specifiedResources: map[string]corev1.ResourceRequirements{
				"mon": {},
			},
			expectedResourceRequirements: corev1.ResourceRequirements{},
		},
		{
			name:            "osd with empty resource requirements",
			daemonName:      "osd",
			resourceProfile: "balanced",
			specifiedResources: map[string]corev1.ResourceRequirements{
				"osd": {},
			},
			expectedResourceRequirements: corev1.ResourceRequirements{},
		},
		{
			name:            "noobaa-core with empty resource requirements",
			daemonName:      "noobaa-core",
			resourceProfile: "balanced",
			specifiedResources: map[string]corev1.ResourceRequirements{
				"noobaa-core": {},
			},
			expectedResourceRequirements: corev1.ResourceRequirements{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sc := &ocsv1.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: ocsv1.StorageClusterSpec{
					ResourceProfile: tc.resourceProfile,
					Resources:       tc.specifiedResources,
				},
			}

			result := getDaemonResources(tc.daemonName, sc)

			// Check requests
			for resource, expected := range tc.expectedResourceRequirements.Requests {
				actual, exists := result.Requests[resource]
				assert.True(t, exists, "Expected request for %s to exist", resource)
				assert.Equal(t, expected.String(), actual.String(), "Request for %s mismatch", resource)
			}

			// Check limits
			for resource, expected := range tc.expectedResourceRequirements.Limits {
				actual, exists := result.Limits[resource]
				assert.True(t, exists, "Expected limit for %s to exist", resource)
				assert.Equal(t, expected.String(), actual.String(), "Limit for %s mismatch", resource)
			}
		})
	}
}

func TestAdjustResource(t *testing.T) {
	testCases := []struct {
		name                         string
		inputResourceRequirements    corev1.ResourceRequirements
		adjustFactor                 float64
		expectedResourceRequirements corev1.ResourceRequirements
	}{
		{
			name: "adjust CPU requests and limits by 50%",
			inputResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
			adjustFactor: 0.5,
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("0.5"),
					corev1.ResourceMemory: resource.MustParse("2Gi"), // memory unchanged
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("4Gi"), // memory unchanged
				},
			},
		},
		{
			name: "adjust CPU with millicores",
			inputResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("999m"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("1500m"),
				},
			},
			adjustFactor: 0.5,
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("499m"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("750m"),
				},
			},
		},
		{
			name: "adjust with different factor",
			inputResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("2"),
				},
			},
			adjustFactor: 0.75,
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("1.5"),
				},
				Limits: corev1.ResourceList{},
			},
		},
		{
			name: "no CPU resources to adjust",
			inputResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
			adjustFactor: 0.5,
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
		},
		{
			name:                      "empty resource requirements",
			inputResourceRequirements: corev1.ResourceRequirements{},
			adjustFactor:              0.5,
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{},
				Limits:   corev1.ResourceList{},
			},
		},
		{
			name: "zero CPU adjustment",
			inputResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("1m"),
				},
			},
			adjustFactor: 0.5,
			expectedResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("0m"),
				},
				Limits: corev1.ResourceList{},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := adjustResource(tc.inputResourceRequirements, tc.adjustFactor)

			// Verify the original is not modified
			assert.NotSame(t, &tc.inputResourceRequirements, &result, "Original should not be modified")

			// Check requests
			if tc.expectedResourceRequirements.Requests == nil {
				assert.Nil(t, result.Requests)
			} else {
				assert.Equal(t, len(tc.expectedResourceRequirements.Requests), len(result.Requests), "Request count mismatch")
				for resource, expected := range tc.expectedResourceRequirements.Requests {
					actual, exists := result.Requests[resource]
					assert.True(t, exists, "Expected request for %s to exist", resource)
					assert.Equal(t, expected.MilliValue(), actual.MilliValue(), "Request for %s mismatch: expected %s, got %s", resource, expected.String(), actual.String())
				}
			}

			// Check limits
			if tc.expectedResourceRequirements.Limits == nil {
				assert.Nil(t, result.Limits)
			} else {
				assert.Equal(t, len(tc.expectedResourceRequirements.Limits), len(result.Limits), "Limit count mismatch")
				for resource, expected := range tc.expectedResourceRequirements.Limits {
					actual, exists := result.Limits[resource]
					assert.True(t, exists, "Expected limit for %s to exist", resource)
					assert.Equal(t, expected.MilliValue(), actual.MilliValue(), "Limit for %s mismatch: expected %s, got %s", resource, expected.String(), actual.String())
				}
			}
		})
	}
}
