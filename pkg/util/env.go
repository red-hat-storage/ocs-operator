package util

import (
	"fmt"
	"os"
)

// MustGetEnv retrieves the value of the environment variable named by key.
// It panics if the environment variable is not set.
func MustGetEnv(key string) string {
	value, ok := os.LookupEnv(key)
	if !ok {
		panic(fmt.Sprintf("environment variable %s is not set", key))
	}
	return value
}
