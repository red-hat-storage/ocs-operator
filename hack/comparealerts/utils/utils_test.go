package utils_test

import (
	"comparealerts/utils"
	"testing"
)

func TestTrimWithOnlySpaces(t *testing.T) {
	testStrings := []string{
		"ab  c",
		`ab
		c`,
		"a\tb\n\nc d",
		"ab \"\tc      \", d   ,   e  , f",
	}
	expectedStrings := []string{
		"ab c",
		"ab c",
		"a b c d",
		`ab " c ", d , e , f`,
	}
	for i, eachTestStr := range testStrings {
		actualResult := utils.TrimWithOnlySpaces(eachTestStr)
		if expectedStrings[i] != actualResult {
			t.Errorf("String: %q \tExpected: %q \tActual: %q",
				eachTestStr, expectedStrings[i], actualResult)
			t.FailNow()
		}
	}
}
