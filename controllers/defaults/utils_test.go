package defaults

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestHalveCpuResource(t *testing.T) {
	testCases := []struct {
		name         string
		input        resource.Quantity
		adjustFactor float64
		expected     resource.Quantity
	}{
		{
			name:     "50% of 1 CPU",
			input:    resource.MustParse("1"),
			adjustFactor: 0.5,
			expected: resource.MustParse("0.5"),
		},
		{
			name:     "50% of 1.5 CPU",
			input:    resource.MustParse("1.5"),
			adjustFactor: 0.5,
			expected: resource.MustParse("0.75"),
		},
		{
			name:     "75% of 1.5 CPU",
			input:    resource.MustParse("1.5"),
			adjustFactor: 0.75,
			expected: resource.MustParse("1.125"),
		},
		{
			name:     "66.67% of 2 CPU",
			input:    resource.MustParse("2"),
			adjustFactor: 0.6667,
			expected: resource.MustParse("1.333"),
		},
		{
			name:     "50% of 999m",
			input:    resource.MustParse("999m"),
			adjustFactor: 0.5,
			expected: resource.MustParse("499m"),
		},
		{
			name:     "33.33% of 100m",
			input:    resource.MustParse("100m"),
			adjustFactor: 0.3333,
			expected: resource.MustParse("33m"),
		},
		{
			name:     "50% of 1m",
			input:    resource.MustParse("1m"),
			adjustFactor: 0.5,
			expected: resource.MustParse("0m"),
		},
		{
			name:     "300% of 0",
			input:    resource.MustParse("0"),
			adjustFactor: 3,
			expected: resource.MustParse("0"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := adjustCpuResource(tc.input, tc.adjustFactor)
			assert.Equal(t, tc.expected.MilliValue(), result.MilliValue(),
				"Expected %s, got %s", tc.expected.String(), result.String())
		})
	}
}
