package storageautoscaler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
