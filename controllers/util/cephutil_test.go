package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetPGBaseUnitSize(t *testing.T) {
	testTable := []struct {
		label                 string
		osdCount              int
		expectedUnitSizeValue int
	}{
		{
			label:                 "Case #1 OSD count is 1,",
			osdCount:              1,
			expectedUnitSizeValue: 64,
		},
		{
			label:                 "Case #2 OSD count is 2",
			osdCount:              2,
			expectedUnitSizeValue: 128,
		},
		{
			label:                 "Case #3 OSD count is 10",
			osdCount:              10,
			expectedUnitSizeValue: 128,
		},
	}
	for _, testCase := range testTable {
		assert.Equal(t, testCase.expectedUnitSizeValue, GetPGBaseUnitSize(testCase.osdCount))
	}
}
