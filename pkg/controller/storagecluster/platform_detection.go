package storagecluster

import (
	"context"
	"fmt"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CloudPlatformType is a string representing cloud platform type. Eg: aws, unknown
type CloudPlatformType string

const (
	// PlatformAWS represents the Amazon Web Services platform
	PlatformAWS CloudPlatformType = "aws"
	// PlatformGCP represents the Google cloud Platform
	PlatformGCP CloudPlatformType = "gcp"
	// PlatformAzure represents the Azure Platform
	PlatformAzure CloudPlatformType = "azure"
	// PlatformUnknown represents an unknown validly formatted cloud platform
	PlatformUnknown CloudPlatformType = "unknown"
)

// ValidCloudPlatforms is a list of all CloudPlatformTypes recognized by the package other than PlatformUnknown
var ValidCloudPlatforms = []CloudPlatformType{PlatformAWS, PlatformGCP, PlatformAzure}

// CloudPlatform is used to get the CloudPlatformType of the running cluster in a thread-safe manner.
type CloudPlatform struct {
	platform CloudPlatformType
	mux      sync.Mutex
}

// GetPlatform is used to get the CloudPlatformType of the running cluster
func (p *CloudPlatform) GetPlatform(c client.Client) (CloudPlatformType, error) {
	// if 'platform' is already set just return it
	if p.platform != "" {
		return p.platform, nil
	}
	p.mux.Lock()
	defer p.mux.Unlock()

	return p.getPlatform(c)
}

func (p *CloudPlatform) getPlatform(c client.Client) (CloudPlatformType, error) {
	nodeList := &corev1.NodeList{}
	err := c.List(context.TODO(), nodeList)
	if err != nil {
		return "", fmt.Errorf("could not get storage nodes to determine cloud platform: %v", err)
	}
	for _, node := range nodeList.Items {
		providerID := node.Spec.ProviderID
		for _, cp := range ValidCloudPlatforms {
			prefix := fmt.Sprintf("%s://", cp)
			if strings.HasPrefix(providerID, prefix) {
				p.platform = cp
				return p.platform, nil
			}
		}
	}
	p.platform = PlatformUnknown
	return p.platform, nil
}

func isValidCloudPlatform(p CloudPlatformType) bool {
	if p == PlatformUnknown {
		return true
	}
	for _, cp := range ValidCloudPlatforms {
		if p == cp {
			return true
		}
	}
	return false
}
