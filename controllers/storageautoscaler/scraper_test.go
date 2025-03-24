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
	syncMap.Store("deviceClass", 0.60)

	log := logr.Logger{}
	objectList := filterObjectsForScaling(&scalerList, syncMap, log)

	assert.Equal(t, 1, len(objectList))
	assert.Equal(t, "ssdscaler", objectList[0].Name)

	syncMap.Store("deviceClass", 0.40)

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
		assert.Equal(t, 0.70, usage)

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
		assert.Equal(t, 0.50, usage)
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
		assert.Equal(t, 0.50, nvmeUsage)
		ssdUsage, _ := syncMap.Load("ssd")
		assert.Equal(t, 0.70, ssdUsage)
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
		assert.Equal(t, 0.70, usage)

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
		assert.Equal(t, 0.70, usage)
	})
}
