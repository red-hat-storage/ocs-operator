package storageautoscaler

import (
	"testing"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestCheckIfScalingNotRequired(t *testing.T) {
	usage := 0.055
	threshold := 6

	required := checkIfScalingRequired(usage, threshold)
	assert.False(t, required)

	usage = 0.05
	threshold = 5

	required = checkIfScalingRequired(usage, threshold)
	assert.True(t, required)

	usage = 0.05
	threshold = 4

	required = checkIfScalingRequired(usage, threshold)
	assert.True(t, required)
}

func TestCalculateExpectedOsdSizeAndCount(t *testing.T) {
	// vertical scaling
	t.Run("vertical scaling", func(t *testing.T) {
		storagecluster := &ocsv1.StorageCluster{
			Spec: ocsv1.StorageClusterSpec{
				StorageDeviceSets: []ocsv1.StorageDeviceSet{
					{
						Count:   3,
						Replica: 3,
						DataPVCTemplate: v1.PersistentVolumeClaim{
							Spec: v1.PersistentVolumeClaimSpec{
								Resources: v1.VolumeResourceRequirements{
									Requests: v1.ResourceList{
										"storage": resource.MustParse("1Ti"),
									},
								},
							},
						},
					},
				},
			},
		}

		storageautoscaler := &ocsv1.StorageAutoScaler{
			Spec: ocsv1.StorageAutoScalerSpec{
				DeviceClass: "ssd",
				MaxOsdSize:  resource.MustParse("1Ti"),
			},
		}

		startOsdSize, expectedOsdSize, startOsdCount, expectedOsdCount, startStorageCapacity, expectedStorageCapacity := calculateExpectedOsdSizeAndCount(storagecluster, storageautoscaler)

		assert.Equal(t, resource.MustParse("1Ti"), startOsdSize)
		assert.Equal(t, resource.MustParse("1Ti"), expectedOsdSize)
		assert.Equal(t, 9, startOsdCount)
		assert.Equal(t, 12, expectedOsdCount)
		testExpectedStorageCapacity := resource.MustParse("12Ti")
		assert.True(t, testExpectedStorageCapacity.Cmp(expectedStorageCapacity) == 0, "Quantities are equal")
		testStartStorageCapacity := resource.MustParse("9Ti")
		assert.True(t, testStartStorageCapacity.Cmp(startStorageCapacity) == 0, "Quantities are equal")
	})

	// horizontal scaling
	t.Run("horizontal scaling", func(t *testing.T) {
		storagecluster := &ocsv1.StorageCluster{
			Spec: ocsv1.StorageClusterSpec{
				StorageDeviceSets: []ocsv1.StorageDeviceSet{
					{
						Count:   3,
						Replica: 3,
						DataPVCTemplate: v1.PersistentVolumeClaim{
							Spec: v1.PersistentVolumeClaimSpec{
								Resources: v1.VolumeResourceRequirements{
									Requests: v1.ResourceList{
										"storage": resource.MustParse("1Ti"),
									},
								},
							},
						},
					},
				},
			},
		}

		storageautoscaler := &ocsv1.StorageAutoScaler{
			Spec: ocsv1.StorageAutoScalerSpec{
				DeviceClass: "ssd",
				MaxOsdSize:  resource.MustParse("4Ti"),
			},
		}

		startOsdSize, expectedOsdSize, startOsdCount, expectedOsdCount, startStorageCapacity, expectedStorageCapacity := calculateExpectedOsdSizeAndCount(storagecluster, storageautoscaler)

		testStartOsdSize := resource.MustParse("1Ti")
		assert.True(t, testStartOsdSize.Cmp(startOsdSize) == 0, "Quantities are equal")
		testExpectedOsdSize := resource.MustParse("2Ti")
		assert.True(t, testExpectedOsdSize.Cmp(expectedOsdSize) == 0, "Quantities are equal")
		assert.Equal(t, 9, startOsdCount)
		assert.Equal(t, 9, expectedOsdCount)
		testExpectedStorageCapacity := resource.MustParse("18Ti")
		assert.True(t, testExpectedStorageCapacity.Cmp(expectedStorageCapacity) == 0, "Quantities are equal")
		testStartStorageCapacity := resource.MustParse("9Ti")
		assert.True(t, testStartStorageCapacity.Cmp(startStorageCapacity) == 0, "Quantities are equal")
	})
}

func TestCheckIfScalingRequired(t *testing.T) {
	usage := 0.055
	threshold := 6

	required := checkIfScalingRequired(usage, threshold)
	assert.False(t, required)

	usage = 0.05
	threshold = 5

	required = checkIfScalingRequired(usage, threshold)
	assert.True(t, required)

	usage = 0.05
	threshold = 4

	required = checkIfScalingRequired(usage, threshold)
	assert.True(t, required)
}
