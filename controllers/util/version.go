package util

import (
	"context"
	"fmt"
	"strings"

	semver "github.com/blang/semver/v4"
	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// TODO: update the fixed in kernel versions
	// supported kernel version corresponding to the OCP version
	CephxKeyRotaionKernelSupportMatrix = map[string]string{
		"4.14": "5.14.0-570.22.1.el9_6.x86_64",
		"4.15": "5.14.0-570.22.1.el9_6.x86_64",
		"4.16": "5.14.0-570.22.1.el9_6.x86_64",
		"4.17": "5.14.0-570.22.1.el9_6.x86_64",
		"4.18": "5.14.0-570.22.1.el9_6.x86_64",
		"4.19": "5.14.0-570.22.1.el9_6.x86_64",
		"4.20": "5.14.0-570.22.1.el9_6.x86_64",
	}

	VersionNotPresentInMatrixErr = fmt.Errorf("ocp version is not present in the given matrix")
)

func GetKernelVersionFromAllNodes(ctx context.Context, cli client.Client) ([]string, error) {

	var nodeList corev1.NodeList
	if err := cli.List(ctx, &nodeList); err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}

	var kernelVersions []string
	for _, node := range nodeList.Items {
		// Check if the node has a kernel version
		if node.Status.NodeInfo.KernelVersion == "" {
			return nil, fmt.Errorf("node %s has empty kernel version", node.Name)
		}
		// Append the kernel version to the slice
		kernelVersions = append(kernelVersions, node.Status.NodeInfo.KernelVersion)
	}

	return kernelVersions, nil
}

func GetMinimumKernelVersion(kernelVersions []string) (string, error) {

	// Check if the slice is empty
	if len(kernelVersions) == 0 {
		return "", fmt.Errorf("no kernel versions provided")
	}

	var minVersion *semver.Version
	var minVersionString string

	for _, version := range kernelVersions {
		v, err := semver.Make(RemoveUnderscores(version))
		if err != nil {
			return "", err
		}

		if minVersion == nil || v.LT(*minVersion) {
			minVersion = &v
			minVersionString = version
		}
	}

	// Return the minimum kernel version
	return minVersionString, nil
}

func IsKernelVersionSupported(currentVersion string, supportedVersion string) (bool, error) {

	parsedCurrentVersion, err := semver.Make(RemoveUnderscores(currentVersion))
	if err != nil {
		return false, err
	}

	parsedSupportedVersion, err := semver.Make(RemoveUnderscores(supportedVersion))
	if err != nil {
		return false, err
	}

	if parsedCurrentVersion.LT(parsedSupportedVersion) {
		return false, nil
	}

	return true, nil
}

func GetKernelVersionForOCPVersion(ocpVersion string, matrix map[string]string) (string, error) {

	parsedOcpVersion, err := semver.Make(ocpVersion)
	if err != nil {
		return "", err
	}

	val, ok := matrix[fmt.Sprintf("%d", parsedOcpVersion.Major)+"."+fmt.Sprintf("%d", parsedOcpVersion.Minor)]

	if !ok {
		return "", VersionNotPresentInMatrixErr
	}

	return val, nil
}

func GetClusterVersion(ctx context.Context, cli client.Client) (string, error) {

	clusterVersion := &configv1.ClusterVersion{}
	err := cli.Get(ctx, types.NamespacedName{Name: "version"}, clusterVersion)
	if err != nil {
		return "", err
	}

	return clusterVersion.Status.Desired.Version, nil
}

func RemoveUnderscores(version string) string {
	return strings.ReplaceAll(version, "_", "")
}
