package openshift

import (
	"github.com/RHsyseng/operator-utils/pkg/platform"
	"k8s.io/client-go/rest"
)

/*
IsOpenShift tests the Kubernetes-based environment for indicators that the running platform is OpenShift.
Accepts <nil> or instantiated 'cfg' rest config parameter.
*/
func IsOpenShift(cfg *rest.Config) (bool, error) {
	return platform.DetectOpenShift(nil, cfg)
}

/*
GetPlatformInfo examines the Kubernetes-based environment and determines the running platform, version, & OS.
Returned PlatformInfo.OCPVersion is derived from underlying PlatformInfo.K8SVersion.
Accepts <nil> or instantiated 'cfg' rest config parameter.

Result: PlatformInfo{ Name: OpenShift, OCPVersion: 4.1, K8SVersion: 1.13+, OS: linux/amd64 }
*/
func GetPlatformInfo(cfg *rest.Config) (platform.PlatformInfo, error) {
	return platform.K8SBasedPlatformVersioner{}.GetPlatformInfo(nil, cfg)
}

/*
LookupOpenShiftVersion refines PlatformInfo.OCPVersion info based on API calls, rather than deriving from K8SVersion.
*** NOTE: OCP 4.1+ requires elevated user permissions, see PlatformVersioner for details
Accepts <nil> or instantiated 'cfg' rest config parameter.
Accepts prepopulated or empty PlatformInfo{}

Result: PlatformInfo{ Name: OpenShift, OCPVersion: 4.1.2, K8SVersion: 1.13+, OS: linux/amd64 }
*/
func LookupOpenShiftVersion(cfg *rest.Config, info platform.PlatformInfo) (platform.PlatformInfo, error) {
	return platform.K8SBasedPlatformVersioner{}.LookupOpenShiftVersion(nil, cfg, info)
}
