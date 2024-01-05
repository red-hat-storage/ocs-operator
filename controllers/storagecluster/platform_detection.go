package storagecluster

import (
	"context"
	"fmt"
	"sync"

	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SkipObjectStorePlatforms is a list of all PlatformTypes where CephObjectStores will not be deployed.
var SkipObjectStorePlatforms = []configv1.PlatformType{
	configv1.AWSPlatformType,
	configv1.GCPPlatformType,
	configv1.AzurePlatformType,
	configv1.IBMCloudPlatformType,
}

// TuneFastPlatforms is a list of all PlatformTypes where TuneFastDeviceClass has to be set True.
var TuneFastPlatforms = []configv1.PlatformType{
	configv1.OvirtPlatformType,
	configv1.IBMCloudPlatformType,
	configv1.AzurePlatformType,
}

// Platform is used to get the CloudPlatformType of the running cluster in a thread-safe manner
type Platform struct {
	platform configv1.PlatformType
	mux      sync.Mutex
}

// GetPlatform is used to get the CloudPlatformType of the running cluster
func (p *Platform) GetPlatform(c client.Client) (configv1.PlatformType, error) {
	// if 'platform' is already set just return it
	if p.platform != "" {
		return p.platform, nil
	}
	p.mux.Lock()
	defer p.mux.Unlock()

	return p.getPlatform(c)
}

func (p *Platform) getPlatform(c client.Client) (configv1.PlatformType, error) {
	infrastructure := &configv1.Infrastructure{ObjectMeta: metav1.ObjectMeta{Name: "cluster"}}
	err := c.Get(context.TODO(), types.NamespacedName{Name: infrastructure.ObjectMeta.Name}, infrastructure)
	if err != nil {
		return "", fmt.Errorf("could not get infrastructure details to determine cloud platform: %v", err)
	}

	p.platform = infrastructure.Status.PlatformStatus.Type

	return p.platform, nil
}

func skipObjectStore(p configv1.PlatformType) bool {
	for _, platform := range SkipObjectStorePlatforms {
		if p == platform {
			return true
		}
	}
	return false
}

func (r *StorageClusterReconciler) DevicesDefaultToFastForThisPlatform() (bool, error) {
	c := r.Client
	platform, err := r.platform.GetPlatform(c)
	if err != nil {
		return false, err
	}

	for _, tfplatform := range TuneFastPlatforms {
		if platform == tfplatform {
			return true, nil
		}
	}

	return false, nil
}

// PlatformsShouldSkipObjectStore determines whether an object store should be created
// for the platform.
func (r *StorageClusterReconciler) PlatformsShouldSkipObjectStore() (bool, error) {
	// Call GetPlatform to get platform
	platform, err := r.platform.GetPlatform(r.Client)
	if err != nil {
		return false, err
	}

	// Call skipObjectStore to skip creation of objectstores
	if skipObjectStore(platform) {
		return true, nil
	}

	return false, nil
}
