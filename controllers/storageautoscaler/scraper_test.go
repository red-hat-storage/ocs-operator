package storageautoscaler

import (
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/prometheus/common/model"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFilterObjectsForScaling(t *testing.T) {
	scalerList := ocsv1.StorageAutoScalerList{
		Items: []ocsv1.StorageAutoScaler{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ssdscaler",
				},
				Spec: ocsv1.StorageAutoScalerSpec{
					DeviceClass:                    "deviceClass",
					StorageScalingThresholdPercent: 50,
				},
			},
		},
	}

	syncMap := &sync.Map{}
	syncMap.Store("deviceClass", OsdUsage{
		TotalUsage:   1.80,
		TotalOsd:     3,
		HighestUsage: 0.60,
	})

	log := logr.Logger{}
	objectList := filterObjectsForScaling(&scalerList, syncMap, log)

	assert.Equal(t, 1, len(objectList))
	assert.Equal(t, "ssdscaler", objectList[0].Name)

	syncMap.Store("deviceClass", OsdUsage{
		TotalUsage:   0.60,
		TotalOsd:     3,
		HighestUsage: 0.40,
	})

	objectList = filterObjectsForScaling(&scalerList, syncMap, log)

	assert.Equal(t, 0, len(objectList))
}

func TestUpdateSyncMap(t *testing.T) {
	t.Run("test sync map update of same device class", func(t *testing.T) {
		syncMap := &sync.Map{}

		var vector model.Vector
		vector = append(vector, &model.Sample{
			Metric: model.Metric{
				"device_class": "ssd",
			},
			Value: 0.70,
		})

		// update the sync map
		updateSyncMap(vector, syncMap)

		// check if the sync map is updated
		usage, _ := syncMap.Load("ssd")
		assert.Equal(t, 0.70, usage.(OsdUsage).HighestUsage)

		// during the second update
		syncMap = &sync.Map{}
		var vector1 model.Vector
		vector1 = append(vector1, &model.Sample{
			Metric: model.Metric{
				"device_class": "ssd",
			},
			Value: 0.50,
		})

		// update the sync map
		updateSyncMap(vector1, syncMap)

		// check if the sync map is updated
		usage, _ = syncMap.Load("ssd")
		assert.Equal(t, 0.50, usage.(OsdUsage).HighestUsage)
	})
	t.Run("test syncmap update with multiple device class", func(t *testing.T) {
		syncMap := &sync.Map{}

		var vector model.Vector
		vector = append(vector, &model.Sample{
			Metric: model.Metric{
				"device_class": "ssd",
			},
			Value: 0.70,
		})
		vector = append(vector, &model.Sample{
			Metric: model.Metric{
				"device_class": "nvme",
			},
			Value: 0.50,
		})
		// update the sync map
		updateSyncMap(vector, syncMap)

		// check if the sync map is updated
		nvmeUsage, _ := syncMap.Load("nvme")
		assert.Equal(t, 0.50, nvmeUsage.(OsdUsage).HighestUsage)
		ssdUsage, _ := syncMap.Load("ssd")
		assert.Equal(t, 0.70, ssdUsage.(OsdUsage).HighestUsage)
	})
	t.Run("test syncmap update with no device class", func(t *testing.T) {
		syncMap := &sync.Map{}

		var vector model.Vector
		vector = append(vector, &model.Sample{
			Metric: model.Metric{
				"device_class": "ssd",
			},
			Value: 0.70,
		})

		// update the sync map
		updateSyncMap(vector, syncMap)

		// check if the sync map is updated
		usage, _ := syncMap.Load("ssd")
		assert.Equal(t, 0.70, usage.(OsdUsage).HighestUsage)

		// during the second update
		syncMap = &sync.Map{}
		var vector1 model.Vector
		vector1 = append(vector1, &model.Sample{
			Metric: model.Metric{},
		})

		// update the sync map
		updateSyncMap(vector1, syncMap)

		usage, _ = syncMap.Load("ssd")
		assert.Equal(t, nil, usage)
	})
	t.Run("test syncmap update with multiple metric values of same device class", func(t *testing.T) {
		syncMap := &sync.Map{}

		var vector model.Vector
		vector = append(vector, &model.Sample{
			Metric: model.Metric{
				"device_class": "ssd",
			},
			Value: 0.70,
		})

		vector = append(vector, &model.Sample{
			Metric: model.Metric{
				"device_class": "ssd",
			},
			Value: 0.60,
		})
		// update the sync map
		updateSyncMap(vector, syncMap)

		// check if the sync map is updated
		usage, _ := syncMap.Load("ssd")
		assert.Equal(t, 0.70, usage.(OsdUsage).HighestUsage)
	})
}

func TestTotalUsageGreaterThanAdjustedThreshold(t *testing.T) {
	t.Run("test total usage greater than threshold minus 10 percent", func(t *testing.T) {
		deviceClassUsage := OsdUsage{
			TotalUsage:   1.80,
			TotalOsd:     3,
			HighestUsage: 0.60,
		}
		result := totalUsageGreaterThanAdjustedThreshold(deviceClassUsage, 50, logr.Logger{}, "deviceClass")
		assert.True(t, result)
	})
	t.Run("test total usage equal to threshold minus 10 percent", func(t *testing.T) {
		deviceClassUsage := OsdUsage{
			TotalUsage:   1.21,
			TotalOsd:     3,
			HighestUsage: 0.40,
		}
		result := totalUsageGreaterThanAdjustedThreshold(deviceClassUsage, 50, logr.Logger{}, "deviceClass")
		assert.True(t, result)
	})
	t.Run("after resize test total usage less than threshold minus 10 percent", func(t *testing.T) {
		// clusterUsage := 1.90/6 // 0.31
		// Threshold = 50, threshold-10 = 40
		// 0.31 < 0.40
		// so it should return false
		deviceClassUsage := OsdUsage{
			TotalUsage:   1.90,
			TotalOsd:     6,
			HighestUsage: 0.55,
		}
		result := totalUsageGreaterThanAdjustedThreshold(deviceClassUsage, 50, logr.Logger{}, "deviceClass")
		assert.False(t, result)
	})
}

func TestIsScalingRequired(t *testing.T) {
	syncMap := &sync.Map{}
	syncMap.Store("deviceClass", OsdUsage{
		TotalUsage:   1.21,
		TotalOsd:     3,
		HighestUsage: 0.60,
	})

	log := logr.Logger{}
	result := isScalingRequired(syncMap, 50, log, "deviceClass")
	assert.True(t, result)

	syncMap.Store("deviceClass", OsdUsage{
		TotalUsage:   0.60,
		TotalOsd:     3,
		HighestUsage: 0.40,
	})

	result = isScalingRequired(syncMap, 50, log, "deviceClass")
	assert.False(t, result)

	syncMap.Store("deviceClass", OsdUsage{
		TotalUsage:   1.0,
		TotalOsd:     3,
		HighestUsage: 0.60,
	})

	result = isScalingRequired(syncMap, 50, log, "deviceClass")
	assert.False(t, result)

	syncMap.Store("deviceClass", OsdUsage{
		TotalUsage:   1.20,
		TotalOsd:     0,
		HighestUsage: 0.60,
	})

	result = isScalingRequired(syncMap, 50, log, "deviceClass")
	assert.False(t, result)

	syncMap.Store("deviceClass", OsdUsage{
		TotalUsage:   1.20,
		TotalOsd:     3,
		HighestUsage: 0.60,
	})

	result = isScalingRequired(syncMap, 0, log, "deviceClass")
	assert.True(t, result)
}

func TestAverageOsdUsage(t *testing.T) {
	deviceClassUsage := OsdUsage{
		TotalUsage:   1.20,
		TotalOsd:     3,
		HighestUsage: 0.60,
	}
	log := logr.Logger{}
	result := averageOsdUsage(deviceClassUsage, log, "deviceClass")
	assert.Equal(t, float64(40), result)

	deviceClassUsage = OsdUsage{
		TotalUsage:   1.20,
		TotalOsd:     0,
		HighestUsage: 0.60,
	}
	result = averageOsdUsage(deviceClassUsage, log, "deviceClass")
	assert.Equal(t, float64(0), result)
}
