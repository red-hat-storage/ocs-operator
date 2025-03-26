package storageautoscaler

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/common/model"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTimeoutHasElapsed(t *testing.T) {
	timeoutSeconds := 100
	startTime := metav1.Time{
		Time: metav1.Now().Time,
	}

	fail := timeoutHasElapsed(timeoutSeconds, startTime)
	assert.False(t, fail)

	timeoutSeconds = 2
	startTime = metav1.Time{
		Time: metav1.Now().Add(-2 * time.Second),
	}

	fail = timeoutHasElapsed(timeoutSeconds, startTime)
	assert.True(t, fail)
}

func TestVerifyVerticalScaling(t *testing.T) {
	// mock getOsdSize() function for test
	mockGetOsdSize = func(ctx context.Context, namespace string, log logr.Logger) (model.Vector, error) {
		return model.Vector{
			&model.Sample{
				Metric: model.Metric{
					"device_class": "hdd",
				},
				Value: model.SampleValue(10),
			},
		}, nil
	}

	defer func() { mockGetOsdSize = getOsdSize }() // Restore after test

	storageAutoScaler := &ocsv1.StorageAutoScaler{
		Spec: ocsv1.StorageAutoScalerSpec{
			DeviceClass: "hdd",
		},
	}

	r := &StorageAutoscalerReconciler{}
	err := r.verifyVerticalScaling(context.Background(), storageAutoScaler, resource.MustParse("10"))
	assert.Nil(t, err)

}
