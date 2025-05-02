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
