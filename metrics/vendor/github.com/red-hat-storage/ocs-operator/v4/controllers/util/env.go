package util

import (
	"fmt"
	"os"
	"strconv"
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

// MustGetEnvInt retrieves the value of the environment variable named by key
// and converts it to an int. It panics if the environment variable is not set
// or if the value cannot be converted to an int.
func MustGetEnvInt(key string) int {
	value := MustGetEnv(key)

	intValue, err := strconv.Atoi(value)
	if err != nil {
		panic(fmt.Sprintf("cannot convert value %q of environment variable %s to int", value, key))
	}
	return intValue
}
