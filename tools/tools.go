package tools

// This package contains import references to packages required only for the
// build process.
// https://github.com/golang/go/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module

import (
	// used for importing make tools provided by OpenShift
	_ "github.com/openshift/build-machinery-go"
)
