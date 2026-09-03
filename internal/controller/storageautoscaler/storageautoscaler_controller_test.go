package storageautoscaler

import (
	"context"
	"testing"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

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
				DeviceClass:          "ssd",
				MaxOsdSize:           resource.MustParse("1Ti"),
				StorageCapacityLimit: resource.MustParse("100Ti"),
			},
		}

		startOsdSize, expectedOsdSize, startOsdCount, expectedOsdCount, startStorageCapacity, expectedStorageCapacity, devicesToAdd, limitReached := calculateExpectedOsdSizeAndCount(storagecluster, storageautoscaler)

		assert.Equal(t, resource.MustParse("1Ti"), startOsdSize)
		assert.Equal(t, resource.MustParse("1Ti"), expectedOsdSize)
		assert.Equal(t, 9, startOsdCount)
		assert.Equal(t, 12, expectedOsdCount)
		assert.Equal(t, 1, devicesToAdd)
		assert.False(t, limitReached)
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
				DeviceClass:          "ssd",
				MaxOsdSize:           resource.MustParse("4Ti"),
				StorageCapacityLimit: resource.MustParse("100Ti"),
			},
		}

		startOsdSize, expectedOsdSize, startOsdCount, expectedOsdCount, startStorageCapacity, expectedStorageCapacity, devicesToAdd, limitReached := calculateExpectedOsdSizeAndCount(storagecluster, storageautoscaler)

		testStartOsdSize := resource.MustParse("1Ti")
		assert.True(t, testStartOsdSize.Cmp(startOsdSize) == 0, "Quantities are equal")
		testExpectedOsdSize := resource.MustParse("2Ti")
		assert.True(t, testExpectedOsdSize.Cmp(expectedOsdSize) == 0, "Quantities are equal")
		assert.Equal(t, 9, startOsdCount)
		assert.Equal(t, 9, expectedOsdCount)
		assert.Equal(t, 0, devicesToAdd)
		assert.False(t, limitReached)
		testExpectedStorageCapacity := resource.MustParse("18Ti")
		assert.True(t, testExpectedStorageCapacity.Cmp(expectedStorageCapacity) == 0, "Quantities are equal")
		testStartStorageCapacity := resource.MustParse("9Ti")
		assert.True(t, testStartStorageCapacity.Cmp(startStorageCapacity) == 0, "Quantities are equal")
	})

	// vertical scaling hits capacity limit
	t.Run("vertical scaling hits capacity limit", func(t *testing.T) {
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
										"storage": resource.MustParse("2Ti"),
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
				DeviceClass:          "ssd",
				MaxOsdSize:           resource.MustParse("8Ti"),
				StorageCapacityLimit: resource.MustParse("20Ti"), // 3*3*2Ti = 18Ti, doubling would be 36Ti > 20Ti
			},
		}

		startOsdSize, expectedOsdSize, startOsdCount, expectedOsdCount, startStorageCapacity, expectedStorageCapacity, devicesToAdd, limitReached := calculateExpectedOsdSizeAndCount(storagecluster, storageautoscaler)

		testStartOsdSize := resource.MustParse("2Ti")
		assert.True(t, testStartOsdSize.Cmp(startOsdSize) == 0, "Start OSD size should be 2Ti")
		// Even though limit is reached, expectedOsdSize is still calculated (but won't be applied)
		testExpectedOsdSize := resource.MustParse("4Ti")
		assert.True(t, testExpectedOsdSize.Cmp(expectedOsdSize) == 0, "Expected OSD size should be 4Ti")
		assert.Equal(t, 9, startOsdCount)
		assert.Equal(t, 9, expectedOsdCount) // count doesn't change in vertical scaling
		assert.Equal(t, 0, devicesToAdd)
		assert.True(t, limitReached) // limit should be reached
		// expectedStorageCapacity shows what it would be if we scaled
		testExpectedStorageCapacity := resource.MustParse("36Ti")
		assert.True(t, testExpectedStorageCapacity.Cmp(expectedStorageCapacity) == 0, "Expected capacity would be 36Ti")
		testStartStorageCapacity := resource.MustParse("18Ti")
		assert.True(t, testStartStorageCapacity.Cmp(startStorageCapacity) == 0, "Start capacity should be 18Ti")
	})

	// horizontal scaling with 10% calculation - large cluster
	t.Run("horizontal scaling with 10% calculation - large cluster", func(t *testing.T) {
		storagecluster := &ocsv1.StorageCluster{
			Spec: ocsv1.StorageClusterSpec{
				StorageDeviceSets: []ocsv1.StorageDeviceSet{
					{
						Count:   50, // large cluster
						Replica: 3,
						DataPVCTemplate: v1.PersistentVolumeClaim{
							Spec: v1.PersistentVolumeClaimSpec{
								Resources: v1.VolumeResourceRequirements{
									Requests: v1.ResourceList{
										"storage": resource.MustParse("8Ti"),
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
				DeviceClass:          "ssd",
				MaxOsdSize:           resource.MustParse("8Ti"), // already at max, so horizontal scaling
				StorageCapacityLimit: resource.MustParse("2000Ti"),
			},
		}

		startOsdSize, expectedOsdSize, startOsdCount, expectedOsdCount, startStorageCapacity, expectedStorageCapacity, devicesToAdd, limitReached := calculateExpectedOsdSizeAndCount(storagecluster, storageautoscaler)

		assert.Equal(t, resource.MustParse("8Ti"), startOsdSize)
		assert.Equal(t, resource.MustParse("8Ti"), expectedOsdSize)
		assert.Equal(t, 150, startOsdCount) // 50*3
		// 10% of 1200Ti (150*8Ti) = 120Ti, 120Ti/8Ti = 15 OSDs, 15/3 replicas = 5 device sets
		// So expectedOsdCount = (50+5)*3 = 165
		assert.Equal(t, 165, expectedOsdCount)
		assert.Equal(t, 5, devicesToAdd) // should add 5 device sets
		assert.False(t, limitReached)
		testExpectedStorageCapacity := resource.MustParse("1320Ti")
		assert.True(t, testExpectedStorageCapacity.Cmp(expectedStorageCapacity) == 0, "Expected capacity should be 1320Ti")
		testStartStorageCapacity := resource.MustParse("1200Ti")
		assert.True(t, testStartStorageCapacity.Cmp(startStorageCapacity) == 0, "Start capacity should be 1200Ti")
	})

	// horizontal scaling that partially hits capacity limit
	t.Run("horizontal scaling partially hits capacity limit", func(t *testing.T) {
		storagecluster := &ocsv1.StorageCluster{
			Spec: ocsv1.StorageClusterSpec{
				StorageDeviceSets: []ocsv1.StorageDeviceSet{
					{
						Count:   50,
						Replica: 3,
						DataPVCTemplate: v1.PersistentVolumeClaim{
							Spec: v1.PersistentVolumeClaimSpec{
								Resources: v1.VolumeResourceRequirements{
									Requests: v1.ResourceList{
										"storage": resource.MustParse("8Ti"),
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
				DeviceClass:          "ssd",
				MaxOsdSize:           resource.MustParse("8Ti"),
				StorageCapacityLimit: resource.MustParse("1250Ti"), // can add some but not full 10%
			},
		}

		startOsdSize, expectedOsdSize, startOsdCount, expectedOsdCount, _, expectedStorageCapacity, devicesToAdd, limitReached := calculateExpectedOsdSizeAndCount(storagecluster, storageautoscaler)

		assert.Equal(t, resource.MustParse("8Ti"), startOsdSize)
		assert.Equal(t, resource.MustParse("8Ti"), expectedOsdSize)
		assert.Equal(t, 150, startOsdCount)
		// Can only add 50Ti (1250-1200), which is 50/8 = 6.25 OSDs, 6/(3 replicas) = 2 device sets
		// So expectedOsdCount = (50+2)*3 = 156
		assert.Equal(t, 156, expectedOsdCount)
		assert.Equal(t, 2, devicesToAdd) // should add only 2 device sets due to limit
		assert.False(t, limitReached)
		testExpectedStorageCapacity := resource.MustParse("1248Ti")
		assert.True(t, testExpectedStorageCapacity.Cmp(expectedStorageCapacity) == 0, "Expected capacity should be 1248Ti")
	})

	// horizontal scaling completely hits capacity limit
	t.Run("horizontal scaling completely hits capacity limit", func(t *testing.T) {
		storagecluster := &ocsv1.StorageCluster{
			Spec: ocsv1.StorageClusterSpec{
				StorageDeviceSets: []ocsv1.StorageDeviceSet{
					{
						Count:   50,
						Replica: 3,
						DataPVCTemplate: v1.PersistentVolumeClaim{
							Spec: v1.PersistentVolumeClaimSpec{
								Resources: v1.VolumeResourceRequirements{
									Requests: v1.ResourceList{
										"storage": resource.MustParse("8Ti"),
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
				DeviceClass:          "ssd",
				MaxOsdSize:           resource.MustParse("8Ti"),
				StorageCapacityLimit: resource.MustParse("1200Ti"), // exactly at current capacity
			},
		}

		startOsdSize, expectedOsdSize, startOsdCount, expectedOsdCount, startStorageCapacity, expectedStorageCapacity, devicesToAdd, limitReached := calculateExpectedOsdSizeAndCount(storagecluster, storageautoscaler)

		assert.Equal(t, resource.MustParse("8Ti"), startOsdSize)
		assert.Equal(t, resource.MustParse("8Ti"), expectedOsdSize)
		assert.Equal(t, 150, startOsdCount)
		// When limit is reached, devicesToAdd is still set to 1, so expectedOsdCount = (50+1)*3 = 153
		assert.Equal(t, 153, expectedOsdCount)
		assert.Equal(t, 1, devicesToAdd)                            // still set to 1 even when limit is reached
		assert.True(t, limitReached)                                // limit should be reached
		testExpectedStorageCapacity := resource.MustParse("1224Ti") // (51*3)*8Ti
		assert.True(t, testExpectedStorageCapacity.Cmp(expectedStorageCapacity) == 0, "Expected capacity would be 1224Ti")
		testStartStorageCapacity := resource.MustParse("1200Ti")
		assert.True(t, testStartStorageCapacity.Cmp(startStorageCapacity) == 0, "Start capacity should be 1200Ti")
	})

	// horizontal scaling minimum 1 device set when 10% is less than 1
	t.Run("horizontal scaling minimum 1 device set", func(t *testing.T) {
		storagecluster := &ocsv1.StorageCluster{
			Spec: ocsv1.StorageClusterSpec{
				StorageDeviceSets: []ocsv1.StorageDeviceSet{
					{
						Count:   5, // small cluster
						Replica: 3,
						DataPVCTemplate: v1.PersistentVolumeClaim{
							Spec: v1.PersistentVolumeClaimSpec{
								Resources: v1.VolumeResourceRequirements{
									Requests: v1.ResourceList{
										"storage": resource.MustParse("8Ti"),
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
				DeviceClass:          "ssd",
				MaxOsdSize:           resource.MustParse("8Ti"),
				StorageCapacityLimit: resource.MustParse("500Ti"),
			},
		}

		startOsdSize, expectedOsdSize, startOsdCount, expectedOsdCount, startStorageCapacity, expectedStorageCapacity, devicesToAdd, limitReached := calculateExpectedOsdSizeAndCount(storagecluster, storageautoscaler)

		assert.Equal(t, resource.MustParse("8Ti"), startOsdSize)
		assert.Equal(t, resource.MustParse("8Ti"), expectedOsdSize)
		assert.Equal(t, 15, startOsdCount) // 5*3
		// 10% of 120Ti (15*8Ti) = 12Ti, 12Ti/8Ti = 1.5 OSDs, 1/3 replicas = 0 device sets
		// But minimum should be 1 device set
		assert.Equal(t, 18, expectedOsdCount) // (5+1)*3 = 18
		assert.Equal(t, 1, devicesToAdd)      // minimum 1 device set
		assert.False(t, limitReached)
		testExpectedStorageCapacity := resource.MustParse("144Ti")
		assert.True(t, testExpectedStorageCapacity.Cmp(expectedStorageCapacity) == 0, "Expected capacity should be 144Ti")
		testStartStorageCapacity := resource.MustParse("120Ti")
		assert.True(t, testStartStorageCapacity.Cmp(startStorageCapacity) == 0, "Start capacity should be 120Ti")
	})

	// edge case: exactly at maxOsdSize from the start
	t.Run("already at maxOsdSize requires horizontal scaling", func(t *testing.T) {
		storagecluster := &ocsv1.StorageCluster{
			Spec: ocsv1.StorageClusterSpec{
				StorageDeviceSets: []ocsv1.StorageDeviceSet{
					{
						Count:   10,
						Replica: 3,
						DataPVCTemplate: v1.PersistentVolumeClaim{
							Spec: v1.PersistentVolumeClaimSpec{
								Resources: v1.VolumeResourceRequirements{
									Requests: v1.ResourceList{
										"storage": resource.MustParse("8Ti"),
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
				DeviceClass:          "ssd",
				MaxOsdSize:           resource.MustParse("8Ti"),
				StorageCapacityLimit: resource.MustParse("500Ti"),
			},
		}

		startOsdSize, expectedOsdSize, startOsdCount, expectedOsdCount, startStorageCapacity, expectedStorageCapacity, devicesToAdd, limitReached := calculateExpectedOsdSizeAndCount(storagecluster, storageautoscaler)

		assert.Equal(t, resource.MustParse("8Ti"), startOsdSize)
		assert.Equal(t, resource.MustParse("8Ti"), expectedOsdSize)
		assert.Equal(t, 30, startOsdCount) // 10*3
		// 10% of 240Ti (30*8Ti) = 24Ti, 24Ti/8Ti = 3 OSDs, 3/3 replicas = 1 device set
		assert.Equal(t, 33, expectedOsdCount) // (10+1)*3
		assert.Equal(t, 1, devicesToAdd)
		assert.False(t, limitReached)
		testExpectedStorageCapacity := resource.MustParse("264Ti")
		assert.True(t, testExpectedStorageCapacity.Cmp(expectedStorageCapacity) == 0, "Expected capacity should be 264Ti")
		testStartStorageCapacity := resource.MustParse("240Ti")
		assert.True(t, testStartStorageCapacity.Cmp(startStorageCapacity) == 0, "Start capacity should be 240Ti")
	})

	// horizontal scaling where 10% calculation results in 1.2 device sets
	t.Run("horizontal scaling with 1.2 device sets calculation", func(t *testing.T) {
		storagecluster := &ocsv1.StorageCluster{
			Spec: ocsv1.StorageClusterSpec{
				StorageDeviceSets: []ocsv1.StorageDeviceSet{
					{
						Count:   12,
						Replica: 3,
						DataPVCTemplate: v1.PersistentVolumeClaim{
							Spec: v1.PersistentVolumeClaimSpec{
								Resources: v1.VolumeResourceRequirements{
									Requests: v1.ResourceList{
										"storage": resource.MustParse("8Ti"),
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
				DeviceClass:          "ssd",
				MaxOsdSize:           resource.MustParse("8Ti"),
				StorageCapacityLimit: resource.MustParse("800Ti"),
			},
		}

		startOsdSize, expectedOsdSize, startOsdCount, expectedOsdCount, startStorageCapacity, expectedStorageCapacity, devicesToAdd, limitReached := calculateExpectedOsdSizeAndCount(storagecluster, storageautoscaler)

		assert.Equal(t, resource.MustParse("8Ti"), startOsdSize)
		assert.Equal(t, resource.MustParse("8Ti"), expectedOsdSize)
		assert.Equal(t, 36, startOsdCount) // 12*3
		// Current capacity: 36*8Ti = 288Ti
		// 10% = 28.8Ti, 28.8Ti/8Ti = 3.6 OSDs, 3.6/3 replicas = 1.2 device sets
		// Integer division rounds down to 2
		assert.Equal(t, 42, expectedOsdCount) // (12+2)*3 = 92
		assert.Equal(t, 2, devicesToAdd)      // should round down to 2
		assert.False(t, limitReached)
		testExpectedStorageCapacity := resource.MustParse("336Ti")
		assert.True(t, testExpectedStorageCapacity.Cmp(expectedStorageCapacity) == 0, "Expected capacity should be 336Ti")
		testStartStorageCapacity := resource.MustParse("288Ti")
		assert.True(t, testStartStorageCapacity.Cmp(startStorageCapacity) == 0, "Start capacity should be 288Ti")
	})
}

func TestDetectInvalidState(t *testing.T) {
	storageclassName := "ocs-storagecluster-ceph-rbd"
	sa := &ocsv1.StorageAutoScaler{}
	storageautoscaler := &ocsv1.StorageAutoScaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "autoscaler",
			Namespace: "namespace",
		},
		Spec: ocsv1.StorageAutoScalerSpec{
			DeviceClass: "ssd",
			StorageCluster: v1.LocalObjectReference{
				Name: "storagecluster",
			},
		},
	}

	storagecluster := &ocsv1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storagecluster",
			Namespace: "namespace",
		},
		Spec: ocsv1.StorageClusterSpec{},
	}

	scheme := runtime.NewScheme()
	assert.NoError(t, ocsv1.AddToScheme(scheme))
	assert.NoError(t, storagev1.AddToScheme(scheme))

	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(storageautoscaler, storagecluster, &ocsv1.StorageAutoScalerList{}).WithStatusSubresource(sa).Build()
	r := &StorageAutoscalerReconciler{
		Client: client,
	}

	// detect DuplicateStorageAutoscaler
	t.Run("detect DuplicateStorageAutoscaler", func(t *testing.T) {
		invalid, err := r.detectInvalidState(context.TODO(), storageautoscaler, storagecluster, "namespace")
		assert.NoError(t, err)
		assert.False(t, invalid)

		// create another StorageAutoScaler
		storageautoscaler2 := &ocsv1.StorageAutoScaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "autoscaler2",
				Namespace: "namespace",
			},
			Spec: ocsv1.StorageAutoScalerSpec{
				DeviceClass: "ssd",
				StorageCluster: v1.LocalObjectReference{
					Name: "storagecluster",
				},
			},
		}

		create := client.Create(context.TODO(), storageautoscaler2)
		assert.NoError(t, create)

		invalid, err = r.detectInvalidState(context.TODO(), storageautoscaler2, storagecluster, "namespace")
		assert.NoError(t, err)
		assert.True(t, invalid)
	})
	t.Run("detect Lean Profile", func(t *testing.T) {
		// update storagecluster with lean profile
		storagecluster.Spec.ResourceProfile = "lean"
		err := client.Update(context.TODO(), storagecluster)
		assert.NoError(t, err)
		invalid, err := r.detectInvalidState(context.TODO(), storageautoscaler, storagecluster, "namespace")
		assert.NoError(t, err)
		assert.True(t, invalid)

	})
	t.Run("detect Lso Storageclass", func(t *testing.T) {
		// update client with lso storageclass
		storageclass := &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: storageclassName,
			},
			Provisioner: "kubernetes.io/no-provisioner",
		}
		err := client.Create(context.TODO(), storageclass)
		assert.NoError(t, err)

		// update storagecluster with lso storageclass
		storagecluster.Spec = ocsv1.StorageClusterSpec{
			StorageDeviceSets: []ocsv1.StorageDeviceSet{
				{
					Count:   3,
					Replica: 3,
					DataPVCTemplate: v1.PersistentVolumeClaim{
						Spec: v1.PersistentVolumeClaimSpec{
							StorageClassName: &storageclassName,
						},
					},
				},
			},
		}

		err = client.Update(context.TODO(), storagecluster)
		assert.NoError(t, err)

		invalid, err := r.detectInvalidState(context.TODO(), storageautoscaler, storagecluster, "namespace")
		assert.NoError(t, err)
		assert.True(t, invalid)
	})
}
