package platform

import (
	"context"
	"errors"
	"log"
	"sync"

	configv1 "github.com/openshift/api/config/v1"
	secv1 "github.com/openshift/api/security/v1"
	configv1Client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	once             sync.Once
	platformInstance *platform

	ErrorPlatformNotDetected = errors.New("platform not detected")

	// SkipObjectStorePlatforms is a list of all PlatformTypes where CephObjectStores will not be deployed.
	SkipObjectStorePlatforms = []configv1.PlatformType{
		configv1.AWSPlatformType,
		configv1.GCPPlatformType,
		configv1.AzurePlatformType,
		configv1.IBMCloudPlatformType,
	}
)

// platform is used to get the PlatformType of the running cluster in a thread-safe manner
// It is a singleton which is initialized exactly once via Detect() function call.
type platform struct {
	isOpenShift bool
	platform    configv1.PlatformType
}

// SetFakePlatformInstanceForTesting can be used to fake a Platform while testing.
// It should only be used for testing. This is not thread-safe.
func SetFakePlatformInstanceForTesting(isOpenShift bool, platformType configv1.PlatformType) {
	platformInstance = &platform{
		isOpenShift: isOpenShift,
		platform:    platformType,
	}
}

// UnsetFakePlatformInstanceForTesting can be used to unset the fake Platform while testing.
// It should only be used for testing. This is not thread-safe.
func UnsetFakePlatformInstanceForTesting() {
	platformInstance = &platform{}
}

// Detect instantiates the platform only once. It is thread-safe.
func Detect() {
	if platformInstance != nil {
		return
	}
	once.Do(func() {
		platformInstance = &platform{}
		cfg := ctrl.GetConfigOrDie()
		dclient := discovery.NewDiscoveryClientForConfigOrDie(cfg)
		_, apis, err := dclient.ServerGroupsAndResources()
		if err != nil && !discovery.IsGroupDiscoveryFailedError(err) {
			log.Fatal(err)
		}

		if discovery.IsGroupDiscoveryFailedError(err) {
			e := err.(*discovery.ErrGroupDiscoveryFailed)
			if _, exists := e.Groups[secv1.GroupVersion]; exists {
				platformInstance.isOpenShift = true
			}
		}

		if !platformInstance.isOpenShift {
			for _, api := range apis {
				if api.GroupVersion == secv1.GroupVersion.String() {
					for _, resource := range api.APIResources {
						if resource.Name == "securitycontextconstraints" {
							platformInstance.isOpenShift = true
							break
						}
					}
				}
				if platformInstance.isOpenShift {
					break
				}
			}
		}

		if platformInstance.isOpenShift {
			if infrastructure, err := configv1client(cfg).Infrastructures().Get(context.TODO(), "cluster", metav1.GetOptions{}); err != nil {
				platformInstance.platform = infrastructure.Status.PlatformStatus.Type
			}
		}
	})
}

func configv1client(cfg *rest.Config) *configv1Client.ConfigV1Client {
	return configv1Client.NewForConfigOrDie(cfg)
}

// IsPlatformOpenShift returns true if platform is detected to be OpenShift.
// It returns false in all other cases, including when platform is not yet detected.
func IsPlatformOpenShift() (bool, error) {
	if platformInstance == nil {
		return false, ErrorPlatformNotDetected
	}
	return platformInstance.isOpenShift, nil
}

// GetPlatformType is used to get the PlatformType of the running cluster.
// It returns a PlatformType only when running on OpenShift clusters.
// If it is not running on OpenShift or platform is not yet detected, it return empty PlatformType.
func GetPlatformType() (configv1.PlatformType, error) {
	if platformInstance == nil {
		return "", ErrorPlatformNotDetected
	}
	return platformInstance.platform, nil
}

// DevicesDefaultToFastForThisPlatform determines whether we should TuneFastDeviceClass for this platform.
// It returns false if we shouldn't TuneFastDeviceClass or if platform is not yet detected.
func DevicesDefaultToFastForThisPlatform() (bool, error) {
	// tuneFastPlatforms is a list of all PlatformTypes where TuneFastDeviceClass has to be set True.
	var tuneFastPlatforms = []configv1.PlatformType{
		configv1.OvirtPlatformType,
		configv1.IBMCloudPlatformType,
		configv1.AzurePlatformType,
	}
	platform, err := GetPlatformType()
	if err != nil {
		return false, err
	}
	for _, tfplatform := range tuneFastPlatforms {
		if platform == tfplatform {
			return true, nil
		}
	}

	return false, nil
}

// PlatformsShouldSkipObjectStore determines whether an object store should be created
// for the platform. It returns false if ObjectStore should not be skipped or if platform is not yet detected.
func PlatformsShouldSkipObjectStore() (bool, error) {
	platform, err := GetPlatformType()
	if err != nil {
		return false, err
	}
	return SkipObjectStore(platform), nil
}

func SkipObjectStore(p configv1.PlatformType) bool {
	for _, platform := range SkipObjectStorePlatforms {
		if p == platform {
			return true
		}
	}
	return false
}
