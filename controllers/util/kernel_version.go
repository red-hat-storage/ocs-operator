package util

import (
	"context"
	"fmt"
	"strings"

	semver "github.com/blang/semver/v4"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type kernelSupportVersion struct {
	// Latest is the kernel version in which the issue was fixed downstream.
	// Any version greater than this is considered to have the fix.
	Latest string

	// Backports contains base kernel version prefixes that received backported fixes.
	// Matching logic: match the first 4 version segments, and the 5th segment should be greater
	// than or equal to the backported entry to consider the fix as present.
	Backports []string
}

var (
	CephxKeyRotaionKernelSupportVersion = kernelSupportVersion{
		Latest:    "5.14.0-570.22.1",
		Backports: []string{"5.14.0-570.22.1"},
	}
)

func GetKernelVersionFromAllNodes(ctx context.Context, cli client.Client) ([]string, error) {

	var nodeList corev1.NodeList
	if err := cli.List(ctx, &nodeList); err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}

	var kernelVersions []string
	var kernelVersionsMap map[string]bool = make(map[string]bool)
	for _, node := range nodeList.Items {
		// Check if the node has a kernel version
		if node.Status.NodeInfo.KernelVersion == "" {
			return nil, fmt.Errorf("node %s has empty kernel version", node.Name)
		}

		if _, ok := kernelVersionsMap[node.Status.NodeInfo.KernelVersion]; !ok {
			kernelVersionsMap[node.Status.NodeInfo.KernelVersion] = true
			// Append the kernel version to the slice
			kernelVersions = append(kernelVersions, node.Status.NodeInfo.KernelVersion)
		}
	}

	return kernelVersions, nil
}

func IsKernelVersionSupported(currentVersion string, supportedVersions kernelSupportVersion) (bool, error) {

	parsedCurrentVersion, err := semver.Make(RemoveUnderscores(currentVersion))
	if err != nil {
		return false, err
	}

	parsedSupportedVersion, err := semver.Make(RemoveUnderscores(supportedVersions.Latest))
	if err != nil {
		return false, err
	}

	if parsedCurrentVersion.GT(parsedSupportedVersion) {
		return true, nil
	}

	for _, supportedVersion := range supportedVersions.Backports {
		parsedSupportedVersion, err := semver.Make(RemoveUnderscores(supportedVersion))
		if err != nil {
			return false, err
		}

		// match first 4 words and 5th one should be greater
		if parsedCurrentVersion.Major == parsedSupportedVersion.Major &&
			parsedCurrentVersion.Minor == parsedSupportedVersion.Minor &&
			parsedCurrentVersion.Patch == parsedSupportedVersion.Patch &&
			parsedCurrentVersion.Pre[0].VersionNum == parsedSupportedVersion.Pre[0].VersionNum &&
			parsedCurrentVersion.Pre[1].VersionNum >= parsedSupportedVersion.Pre[1].VersionNum {
			return true, nil
		}
	}

	return false, nil
}

func AreKernelVersionsSupported(currentVersions []string, supportedVersions kernelSupportVersion) (bool, error) {

	for _, kversion := range currentVersions {
		if isSupported, err := IsKernelVersionSupported(kversion, supportedVersions); !isSupported || err != nil {
			return isSupported, err
		}
	}

	return true, nil
}

func RemoveUnderscores(version string) string {
	return strings.ReplaceAll(version, "_", "")
}
