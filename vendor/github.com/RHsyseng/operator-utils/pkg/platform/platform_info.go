package platform

import (
	"fmt"
	"strings"
)

type PlatformType string

const (
	OpenShift  PlatformType = "OpenShift"
	Kubernetes PlatformType = "Kubernetes"
)

type PlatformInfo struct {
	Name       PlatformType `json:"name"`
	OCPVersion string       `json:"ocpVersion"`
	K8SVersion string       `json:"k8sVersion"`
	OS         string       `json:"os"`
}

func (info PlatformInfo) K8SMajorVersion() string {
	return strings.Split(info.K8SVersion, ".")[0]
}

func (info PlatformInfo) K8SMinorVersion() string {
	return strings.Split(info.K8SVersion, ".")[1]
}

func (info PlatformInfo) OCPMajorVersion() string {
	return strings.Split(info.OCPVersion, ".")[0]
}

func (info PlatformInfo) OCPMinorVersion() string {
	return strings.Split(info.OCPVersion, ".")[1]
}

func (info PlatformInfo) OCPBuildVersion() string {
	return strings.Join(strings.Split(info.OCPVersion, ".")[2:], ".")
}

func (info PlatformInfo) IsOpenShift() bool {
	return info.Name == OpenShift
}

func (info PlatformInfo) IsKubernetes() bool {
	return info.Name == Kubernetes
}

func (info *PlatformInfo) ApproximateOpenShiftVersion() {

	if info.K8SVersion == "" || info.Name == Kubernetes {
		return
	}
	switch info.K8SVersion {
	case "1.10+":
		info.OCPVersion = "3.10"
	case "1.11+":
		info.OCPVersion = "3.11"
	case "1.13+":
		info.OCPVersion = "4.1"
	default:
		log.Info("unable to version-match OCP to K8S version " + info.K8SVersion)
		info.OCPVersion = ""
		return
	}
}

func (info PlatformInfo) String() string {
	return "PlatformInfo [" +
		"Name: " + fmt.Sprintf("%v", info.Name) +
		", OCPVersion: " + info.OCPVersion +
		", K8SVersion: " + info.K8SVersion +
		", OS: " + info.OS + "]"
}

// full generated 'version' API fetch result struct @
// gist.github.com/jeremyary/5a66530611572a057df7a98f3d2902d5
type PlatformClusterInfo struct {
	Status struct {
		Desired struct {
			Version string `json:"version"`
		} `json:"desired"`
	} `json:"status"`
}
