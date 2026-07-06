package storagecluster

import (
	"context"
	"testing"

	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"

	"github.com/stretchr/testify/assert"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetLocalVolumeAttributesClassNames(t *testing.T) {
	ctx := context.Background()
	scheme := createFakeScheme(t)

	tests := []struct {
		name          string
		existingVACs  []client.Object
		expectedNames []string
		expectedCount int
		expectSorted  bool
		expectErr     bool
	}{
		{
			name:          "no VACs in cluster",
			existingVACs:  []client.Object{},
			expectedNames: []string{},
			expectedCount: 0,
		},
		{
			name: "single RBD VAC",
			existingVACs: []client.Object{
				&storagev1.VolumeAttributesClass{
					ObjectMeta: metav1.ObjectMeta{Name: "rbd-qos-high"},
					DriverName: util.RbdDriverName,
					Parameters: map[string]string{"maxReadIops": "2000"},
				},
			},
			expectedNames: []string{"rbd-qos-high"},
			expectedCount: 1,
		},
		{
			name: "multiple RBD VACs sorted",
			existingVACs: []client.Object{
				&storagev1.VolumeAttributesClass{
					ObjectMeta: metav1.ObjectMeta{Name: "rbd-qos-low"},
					DriverName: util.RbdDriverName,
					Parameters: map[string]string{"maxReadIops": "500"},
				},
				&storagev1.VolumeAttributesClass{
					ObjectMeta: metav1.ObjectMeta{Name: "rbd-qos-high"},
					DriverName: util.RbdDriverName,
					Parameters: map[string]string{"maxReadIops": "2000"},
				},
			},
			expectedNames: []string{"rbd-qos-high", "rbd-qos-low"},
			expectedCount: 2,
			expectSorted:  true,
		},
		{
			name: "filters out non-RBD drivers",
			existingVACs: []client.Object{
				&storagev1.VolumeAttributesClass{
					ObjectMeta: metav1.ObjectMeta{Name: "rbd-qos"},
					DriverName: util.RbdDriverName,
					Parameters: map[string]string{"maxReadIops": "1000"},
				},
				&storagev1.VolumeAttributesClass{
					ObjectMeta: metav1.ObjectMeta{Name: "cephfs-qos"},
					DriverName: util.CephFSDriverName,
					Parameters: map[string]string{"maxReadBps": "100000"},
				},
				&storagev1.VolumeAttributesClass{
					ObjectMeta: metav1.ObjectMeta{Name: "nfs-qos"},
					DriverName: util.NfsDriverName,
					Parameters: map[string]string{"maxReadIops": "500"},
				},
				&storagev1.VolumeAttributesClass{
					ObjectMeta: metav1.ObjectMeta{Name: "ebs-qos"},
					DriverName: "ebs.csi.aws.com",
					Parameters: map[string]string{"maxReadIops": "1000"},
				},
			},
			expectedNames: []string{"rbd-qos"},
			expectedCount: 1,
		},
		{
			name: "excludes VACs with external class label",
			existingVACs: []client.Object{
				&storagev1.VolumeAttributesClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rbd-qos-internal",
					},
					DriverName: util.RbdDriverName,
					Parameters: map[string]string{"maxReadIops": "1000"},
				},
				&storagev1.VolumeAttributesClass{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "rbd-qos-external",
						Labels: map[string]string{util.ExternalClassLabelKey: "true"},
					},
					DriverName: util.RbdDriverName,
					Parameters: map[string]string{"maxReadIops": "2000"},
				},
			},
			expectedNames: []string{"rbd-qos-internal"},
			expectedCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existingVACs...).
				Build()

			result, err := getLocalVolumeAttributesClassNames(ctx, kubeClient)
			if tt.expectErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedCount, len(result), "unexpected number of VACs")

			names := make([]string, len(result))
			for i, vac := range result {
				names[i] = vac.Name
			}
			assert.Equal(t, tt.expectedNames, names)
		})
	}
}
