package storagecluster

import (
	"context"
	"fmt"
	"sync"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/red-hat-storage/ocs-operator/controllers/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	//IBMCloud COS[Cloud Object Storage] secret name
	ibmCloudCosSecretName = "ibm-cloud-cos-creds"

	//IBMCloudPlatform with COS Secret
	IBMCloudCosPlatformType configv1.PlatformType = "IBMCloudCosPlatform"
)

// SkipObjectStorePlatforms is a list of all PlatformTypes where CephObjectStores will not be deployed.
var SkipObjectStorePlatforms = []configv1.PlatformType{
	configv1.AWSPlatformType,
	configv1.GCPPlatformType,
	configv1.AzurePlatformType,
	IBMCloudCosPlatformType,
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

	p.platform = infrastructure.Status.Platform //nolint:staticcheck

	// if IBMCloudPlatformType check for COS secret in cluster
	if p.platform == configv1.IBMCloudPlatformType {
		platformIBM, platErr := getActualIBMPlatformType(c)
		if platErr != nil {
			return "", fmt.Errorf("Error checking COS secret in IBMCloud: %v", platErr)
		}
		p.platform = platformIBM
	}

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

func getActualIBMPlatformType(c client.Client) (configv1.PlatformType, error) {
	isSecretPresent, secErr := IsCosSecretPresent(c)
	if secErr != nil {
		return "", fmt.Errorf("Error checking COS secret in IBMCloud: %v", secErr)
	}
	if isSecretPresent {
		// IsIBMCloud and COS Secret present.
		// return new CloudProviderType
		return IBMCloudCosPlatformType, nil
	}
	//COS secret is not present in IBMCloudPlatform
	return configv1.IBMCloudPlatformType, nil
}

// IsCosSecretPresent checks for ibm-cos-cred secret in the concerned namespace
// if platform is IBMCloud, enable CephObjectStore only if ibm-cloud-cos-creds secret is not present
// in the target namespace
func IsCosSecretPresent(c client.Client) (bool, error) {
	// TODO: better way to get target namespace
	ns, nsErr := util.GetWatchNamespace()
	if nsErr != nil {
		return false, nsErr
	}
	foundSecret := &corev1.Secret{}
	err := c.Get(context.TODO(), types.NamespacedName{Name: ibmCloudCosSecretName, Namespace: ns}, foundSecret)
	if err != nil && errors.IsNotFound(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	// Secret is present.
	return true, nil
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
