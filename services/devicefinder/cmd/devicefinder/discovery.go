package main

import (
	"fmt"
	"runtime"

	"github.com/red-hat-storage/ocs-operator/v4/services/devicefinder/devicefinder/discovery"
	"k8s.io/klog/v2"
)

func startDeviceDiscovery() error {
	printVersion()

	discoveryObj, err := discovery.NewDeviceDiscovery()
	if err != nil {
		return fmt.Errorf("failed to discover devices: %w", err)
	}

	err = discoveryObj.Start()
	if err != nil {
		return fmt.Errorf("failed to discover devices: %w", err)
	}

	return nil
}

func printVersion() {
	klog.Infof("Go Version: %s", runtime.Version())
	klog.Infof("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH)
}
