/*
Copyright 2020 Red Hat OpenShift Container Storage.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"os"
	"time"

	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	quotav1 "github.com/openshift/api/quota/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StorageClusterSpec defines the desired state of StorageCluster
type StorageClusterSpec struct {
	ManageNodes  bool   `json:"manageNodes,omitempty"`
	InstanceType string `json:"instanceType,omitempty"`
	// LabelSelector is used to specify custom labels of nodes to run OCS on
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
	// ExternalStorage is optional and defaults to false. When set to true, OCS will
	// connect to an external OCS Storage Cluster instead of provisioning one locally.
	ExternalStorage ExternalStorageClusterSpec `json:"externalStorage,omitempty"`
	// HostNetwork defaults to false
	HostNetwork bool `json:"hostNetwork,omitempty"`
	// Placement is optional and used to specify placements of OCS components explicitly
	Placement rookCephv1.PlacementSpec `json:"placement,omitempty"`
	// Resources follows the conventions of and is mapped to CephCluster.Spec.Resources
	Resources map[string]corev1.ResourceRequirements `json:"resources,omitempty"`
	// Resource Profile can be used to choose from a set of predefined resource profiles for the ceph daemons.
	// We have 3 profiles
	// lean: suitable for clusters with limited resources,
	// balanced: suitable for most use cases,
	// performance: suitable for clusters with high amount of resources.
	// +kubebuilder:validation:Enum=lean;Lean;balanced;Balanced;performance;Performance
	ResourceProfile    string                        `json:"resourceProfile,omitempty"`
	Encryption         EncryptionSpec                `json:"encryption,omitempty"`
	StorageDeviceSets  []StorageDeviceSet            `json:"storageDeviceSets,omitempty"`
	MonPVCTemplate     *corev1.PersistentVolumeClaim `json:"monPVCTemplate,omitempty"`
	MonDataDirHostPath string                        `json:"monDataDirHostPath,omitempty"`
	Mgr                *MgrSpec                      `json:"mgr,omitempty"`
	MultiCloudGateway  *MultiCloudGatewaySpec        `json:"multiCloudGateway,omitempty"`
	NFS                *NFSSpec                      `json:"nfs,omitempty"`
	CSI                *CSIDriverSpec                `json:"csi,omitempty"`
	// Monitoring controls the configuration of resources for exposing OCS metrics
	Monitoring *MonitoringSpec `json:"monitoring,omitempty"`
	// Version specifies the version of StorageCluster
	// +kubebuilder:deprecatedversion:warning="This field has been deprecated and will be removed in future versions. Use `StorageCluster.Status.Version` instead."
	Version string `json:"version,omitempty"`
	// Network represents cluster network settings
	Network *rookCephv1.NetworkSpec `json:"network,omitempty"`
	// ManagedResources specifies how to deal with auxiliary resources reconciled
	// with the StorageCluster
	ManagedResources ManagedResourcesSpec `json:"managedResources,omitempty"`
	// If enabled, sets the failureDomain to host, allowing devices to be
	// distributed evenly across all nodes, regardless of distribution in zones
	// or racks.
	FlexibleScaling bool `json:"flexibleScaling,omitempty"`
	// NodeTopologies specifies the nodes available for the storage cluster,
	// preferred failure domain and location for the arbiter resources. This is
	// optional for non-arbiter clusters. For arbiter clusters, the
	// arbiterLocation is required; failure domain and the node labels are
	// optional. When the failure domain and the node labels are missing, the
	// ocs-operator makes a best effort to determine them automatically.
	NodeTopologies *NodeTopologyMap `json:"nodeTopologies,omitempty"`
	// ArbiterSpec specifies the storage cluster options related to arbiter.
	// If Arbiter is enabled, ArbiterLocation in the NodeTopologies must be specified.
	Arbiter ArbiterSpec `json:"arbiter,omitempty"`
	// Mirroring specifies data mirroring configuration for the storage cluster.
	// This configuration will only be applied to resources managed by the operator.
	Mirroring *MirroringSpec `json:"mirroring,omitempty"`
	// OverprovisionControl specifies the allowed hard-limit PVs overprovisioning relative to
	// the effective usable storage capacity.
	OverprovisionControl []OverprovisionControlSpec `json:"overprovisionControl,omitempty"`

	// AllowRemoteStorageConsumers Indicates that the OCS cluster should deploy the needed
	// components to enable connections from remote consumers.
	AllowRemoteStorageConsumers bool `json:"allowRemoteStorageConsumers,omitempty"`

	// ProviderAPIServerServiceType Indicates the ServiceType for OCS Provider API Server Service.
	// The supported values are NodePort or LoadBalancer. The default ServiceType is NodePort if the value is empty.
	// This will only be used when AllowRemoteStorageConsumers is set to true
	ProviderAPIServerServiceType corev1.ServiceType `json:"providerAPIServerServiceType,omitempty"`

	// EnableCephTools toggles on whether or not the ceph tools pod
	// should be deployed.
	// Defaults to false
	// +optional
	EnableCephTools bool `json:"enableCephTools,omitempty"`

	// Logging represents loggings settings
	// +optional
	// +nullable
	LogCollector *rookCephv1.LogCollectorSpec `json:"logCollector,omitempty"`

	// BackingStorageClasses is a list of storage classes that will be
	// provisioned by the storagecluster controller to be used in
	// storageDeviceSets section of the CR.
	BackingStorageClasses []BackingStorageClass `json:"backingStorageClasses,omitempty"`
	// DefaultStorageProfile is the default storage profile to use for
	// the storagerequest as StorageProfile is optional.
	DefaultStorageProfile string `json:"defaultStorageProfile,omitempty"`
}

// CSIDriverSpec defines the CSI driver settings for the StorageCluster.
type CSIDriverSpec struct {
	// ReadAffinity defines the read affinity settings for CSI driver.
	// +kubebuilder:validation:Optional
	ReadAffinity *rookCephv1.ReadAffinitySpec `json:"readAffinity,omitempty"`
}

// BackingStorageClass defines the backing storageclass for StorageDeviceSet
type BackingStorageClass struct {
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Provisioner indicates the type of the provisioner.
	// +optional
	Provisioner string `json:"provisioner,omitempty"`

	// Parameters holds the parameters for the provisioner that should
	// create volumes of this storage class.
	// +optional
	Parameters map[string]string `json:"parameters,omitempty"`
}

// KeyManagementServiceSpec provides a way to enable KMS
type KeyManagementServiceSpec struct {
	// +optional
	Enable bool `json:"enable,omitempty"`
}

// ManagedResourcesSpec defines how to reconcile auxiliary resources
type ManagedResourcesSpec struct {
	CephCluster           ManageCephCluster           `json:"cephCluster,omitempty"`
	CephConfig            ManageCephConfig            `json:"cephConfig,omitempty"`
	CephDashboard         ManageCephDashboard         `json:"cephDashboard,omitempty"`
	CephBlockPools        ManageCephBlockPools        `json:"cephBlockPools,omitempty"`
	CephNonResilientPools ManageCephNonResilientPools `json:"cephNonResilientPools,omitempty"`
	CephFilesystems       ManageCephFilesystems       `json:"cephFilesystems,omitempty"`
	CephObjectStores      ManageCephObjectStores      `json:"cephObjectStores,omitempty"`
	CephObjectStoreUsers  ManageCephObjectStoreUsers  `json:"cephObjectStoreUsers,omitempty"`
	CephToolbox           ManageCephToolbox           `json:"cephToolbox,omitempty"`
	CephRBDMirror         ManageCephRBDMirror         `json:"cephRBDMirror,omitempty"`
}

// ManageCephCluster defines how to reconcile the Ceph cluster definition
type ManageCephCluster struct {
	ReconcileStrategy string `json:"reconcileStrategy,omitempty"`
	// +kubebuilder:validation:Enum=1;2
	MgrCount int `json:"mgrCount,omitempty"`
	// +kubebuilder:validation:Enum=3;5
	MonCount int `json:"monCount,omitempty"`
	// WaitTimeoutForHealthyOSDInMinutes defines the time the operator would wait before an OSD can be stopped for upgrade or restart.
	// If `continueUpgradeAfterChecksEvenIfNotHealthy` is `false` and the timeout exceeds and OSD is not ok to stop, then the operator
	// would skip upgrade for the current OSD and proceed with the next one.
	// If `continueUpgradeAfterChecksEvenIfNotHealthy` is `true`, then operator would continue with the upgrade of an OSD even if its
	// not ok to stop after the timeout.
	// This timeout won't be applied if `skipUpgradeChecks` is `true`.
	// The default wait timeout is 10 minutes.
	WaitTimeoutForHealthyOSDInMinutes time.Duration `json:"waitTimeoutForHealthyOSDInMinutes,omitempty"`
	// Whether or not upgrade should continue even if a check fails
	// This means Ceph's status could be degraded and we don't recommend upgrading but you might decide otherwise
	// Use at your OWN risk
	SkipUpgradeChecks bool `json:"skipUpgradeChecks,omitempty"`
	// Whether or not continue if PGs are not clean during an upgrade
	ContinueUpgradeAfterChecksEvenIfNotHealthy *bool `json:"continueUpgradeAfterChecksEvenIfNotHealthy,omitempty"`
	// Whether or not requires PGs are clean before an OSD upgrade. If set to `true` OSD upgrade process won't start until PGs are healthy.
	// This configuration will be ignored if `skipUpgradeChecks` is `true`.
	UpgradeOSDRequiresHealthyPGs bool `json:"upgradeOSDRequiresHealthyPGs,omitempty"`
	// A duration in minutes that determines how long an entire failureDomain like `region/zone/host` will be held in `noout` (in addition to the
	// default DOWN/OUT interval) when it is draining. This is only relevant when  `managePodBudgets` is `true` in cephCluster CR.
	// The default value is `30` minutes.
	OsdMaintenanceTimeout time.Duration `json:"osdMaintenanceTimeout,omitempty"`
	// NearFullRatio is the ratio at which the cluster is considered nearly full and will raise a ceph health warning. Default is 0.75.
	// +kubebuilder:validation:Minimum=0.0
	// +kubebuilder:validation:Maximum=1.0
	// +nullable
	NearFullRatio *float64 `json:"nearFullRatio,omitempty"`
	// BackfillFullRatio is the ratio at which the cluster is too full for backfill. Backfill will be disabled if above this threshold. Default is 0.80.
	// +kubebuilder:validation:Minimum=0.0
	// +kubebuilder:validation:Maximum=1.0
	// +nullable
	BackfillFullRatio *float64 `json:"backfillFullRatio,omitempty"`
	// FullRatio is the ratio at which the cluster is considered full and ceph will stop accepting writes. Default is 0.85.
	// +kubebuilder:validation:Minimum=0.0
	// +kubebuilder:validation:Maximum=1.0
	// +nullable
	FullRatio *float64 `json:"fullRatio,omitempty"`

	// Whether to allow updating the device class after the OSD is initially provisioned
	AllowDeviceClassUpdate bool `json:"allowDeviceClassUpdate,omitempty"`

	// CephClusterHealthCheckSpec represent the healthcheck for Ceph daemons
	HealthCheck *rookCephv1.CephClusterHealthCheckSpec `json:"healthCheck,omitempty"`
}

// ManageCephConfig defines how to reconcile the Ceph configuration
type ManageCephConfig struct {
	ReconcileStrategy string `json:"reconcileStrategy,omitempty"`
}

// ManageCephDashboard defines how to reconcile Ceph dashboard
type ManageCephDashboard struct {
	Enable bool `json:"enable,omitempty"`
	// serve the dashboard using SSL
	SSL bool `json:"ssl,omitempty"`
}

// ManageCephBlockPools defines how to reconcile CephBlockPools
type ManageCephBlockPools struct {
	ReconcileStrategy    string `json:"reconcileStrategy,omitempty"`
	DisableStorageClass  bool   `json:"disableStorageClass,omitempty"`
	DisableSnapshotClass bool   `json:"disableSnapshotClass,omitempty"`
	// if set to true, the storageClass created for cephBlockPools will be annotated as the default for the whole cluster
	DefaultStorageClass bool `json:"defaultStorageClass,omitempty"`
	// StorageClassName specifies the name of the storage class created for ceph block pools
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$
	StorageClassName string `json:"storageClassName,omitempty"`
	// VirtualizationStorageClassName specifies the name of the storage class created for ceph block pools
	// for virtualization environment
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$
	VirtualizationStorageClassName string `json:"virtualizationStorageClassName,omitempty"`
	// PoolSpec specifies the pool specification for the default cephBlockPool
	PoolSpec rookCephv1.PoolSpec `json:"poolSpec,omitempty"`
}

// ManageCephNonResilientPools defines how to reconcile ceph non-resilient pools
type ManageCephNonResilientPools struct {
	Enable bool `json:"enable,omitempty"`
	// Count is the number of devices in this set
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	Count int `json:"count,omitempty"`
	// ResourceRequirements (requests/limits) for the devices
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// VolumeClaimTemplates is a PVC template for the underlying storage devices
	VolumeClaimTemplate corev1.PersistentVolumeClaim `json:"volumeClaimTemplate,omitempty"`
	// StorageClassName specifies the name of the storage class created for ceph non-resilient pools
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$
	StorageClassName string `json:"storageClassName,omitempty"`
	// Parameters is a list of properties to enable on the non-resilient cephBlockPools
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	// +nullable
	Parameters map[string]string `json:"parameters,omitempty"`
}

// ManageCephFilesystems defines how to reconcile CephFilesystems
type ManageCephFilesystems struct {
	ReconcileStrategy     string `json:"reconcileStrategy,omitempty"`
	DisableStorageClass   bool   `json:"disableStorageClass,omitempty"`
	ActiveMetadataServers int    `json:"activeMetadataServers,omitempty"`
	DisableSnapshotClass  bool   `json:"disableSnapshotClass,omitempty"`
	// StorageClassName specifies the name of the storage class created for cephfs
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$
	StorageClassName string `json:"storageClassName,omitempty"`
	// MetadataPoolSpec specifies the pool specification for the default cephFS metadata pool
	MetadataPoolSpec rookCephv1.PoolSpec `json:"metadataPoolSpec,omitempty"`
	// DataPoolSpec specifies the pool specification for the default cephfs data pool
	DataPoolSpec rookCephv1.PoolSpec `json:"dataPoolSpec,omitempty"`
	// AdditionalDataPools specifies list of additional named cephfs data pools
	AdditionalDataPools []rookCephv1.NamedPoolSpec `json:"additionalDataPools,omitempty"`
}

// ManageCephObjectStores defines how to reconcile CephObjectStores
type ManageCephObjectStores struct {
	ReconcileStrategy   string `json:"reconcileStrategy,omitempty"`
	DisableStorageClass bool   `json:"disableStorageClass,omitempty"`
	GatewayInstances    int    `json:"gatewayInstances,omitempty"`
	DisableRoute        bool   `json:"disableRoute,omitempty"`
	HostNetwork         *bool  `json:"hostNetwork,omitempty"`
	// StorageClassName specifies the name of the storage class created for ceph obc's
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$
	StorageClassName string `json:"storageClassName,omitempty"`
	// MetadataPoolSpec specifies the pool specification for the default cephObjectStore metadata pool
	MetadataPoolSpec rookCephv1.PoolSpec `json:"metadataPoolSpec,omitempty"`
	// DataPoolSpec specifies the pool specification for the default cephObjectStore data pool
	DataPoolSpec rookCephv1.PoolSpec `json:"dataPoolSpec,omitempty"`
}

// ManageCephObjectStoreUsers defines how to reconcile CephObjectStoreUsers
type ManageCephObjectStoreUsers struct {
	ReconcileStrategy string `json:"reconcileStrategy,omitempty"`
}

// ManageCephToolbox defines how to reconcile Ceph toolbox
type ManageCephToolbox struct {
	ReconcileStrategy string `json:"reconcileStrategy,omitempty"`
}

// ManageCephRBDMirror defines how to reconcile Ceph RBDMirror
type ManageCephRBDMirror struct {
	ReconcileStrategy string `json:"reconcileStrategy,omitempty"`
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	DaemonCount int `json:"daemonCount,omitempty"`
}

// MgrSpec defines the settings for the Ceph Manager
type MgrSpec struct {
	// EnableActivePassive can be set as true to deploy 2 ceph manager pods, one active and one standby
	// Ceph will promote the standby mgr when the active mgr goes down due to any reason
	// +kubebuilder:deprecatedversion:warning="This field has been deprecated and will be removed in future. By default we now have 2 ceph manager pods, one active and one standby."
	EnableActivePassive bool `json:"enableActivePassive,omitempty"`
}

// ExternalStorageKind specifies a kind of the external storage
type ExternalStorageKind string

const (
	// KindOCS specifies a "ocs" kind of the external storage
	KindOCS ExternalStorageKind = "ocs"

	// KindRHCS specifies a "rhcs" kind of the external storage
	KindRHCS ExternalStorageKind = "rhcs"
)

// ExternalStorageClusterSpec defines the spec of the external Storage Cluster
// to be connected to the local cluster
type ExternalStorageClusterSpec struct {
	// +optional
	Enable bool `json:"enable,omitempty"`
}

// StorageDeviceSet defines a set of storage devices.
// It configures the StorageClassDeviceSets field in Rook-Ceph.
type StorageDeviceSet struct {
	// Count is the number of devices in each StorageClassDeviceSet
	// +kubebuilder:validation:Minimum=1
	Count int `json:"count"`

	// Replica is the number of StorageClassDeviceSets for this
	// StorageDeviceSet
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replica int `json:"replica,omitempty"`

	// DeviceType is the value of device type in
	// this StorageDeviceSet. It can have one of the
	// three values (SSD, HDD, NVMe)
	// +kubebuilder:validation:Enum=SSD;ssd;HDD;hdd;NVMe;NVME;nvme
	// +optional
	DeviceType string `json:"deviceType,omitempty"`

	// DeviceClass is an optional, fine-grained property of DeviceType.
	// If non empty, it defines the 'crushDeviceClass' value as used by ceph's
	// CRUSH map. If empty, then operator will set 'crushDeviceClass' to SSD and
	// 'TuneFastDeviceClass' to true
	// +optional
	DeviceClass string `json:"deviceClass,omitempty"`

	// InitialWeight is an optional explicit OSD weight value in TiB units.
	// If non empty, it defines the 'CrushInitialWeight' value which is
	// assigned to ceph OSD upon init
	// +kubebuilder:validation:Pattern=`^([0-9]*[.])?[0-9]+(Ti[B])$`
	// +optional
	InitialWeight string `json:"initialWeight,omitempty"`

	// PrimaryAffinity is an optional OSD primary-affinity value within the
	// range [0,1). This value influence the way Ceph's CRUSH selection of
	// primary OSDs. Lower value reduce performance bottlenecks (especially
	// on read operations). If not set, default value is 1.
	// https://docs.ceph.com/en/latest/rados/operations/crush-map/#primary-affinity
	// +kubebuilder:validation:Pattern=`^0.[0-9]+$`
	// +optional
	PrimaryAffinity string `json:"primaryAffinity,omitempty"`

	// TopologyKey is the Kubernetes topology label that the
	// StorageClassDeviceSets will be distributed across. Ignored if
	// Placement is set
	// +optional
	TopologyKey string `json:"topologyKey,omitempty"`

	// Portable says whether the OSDs in this device set can move between
	// nodes. This is ignored if Placement is not set
	// +optional
	Portable bool `json:"portable,omitempty"`

	Name                string                        `json:"name"`
	Resources           corev1.ResourceRequirements   `json:"resources,omitempty"`
	PreparePlacement    rookCephv1.Placement          `json:"preparePlacement,omitempty"`
	Placement           rookCephv1.Placement          `json:"placement,omitempty"`
	Config              StorageDeviceSetConfig        `json:"config,omitempty"`
	DataPVCTemplate     corev1.PersistentVolumeClaim  `json:"dataPVCTemplate"`
	MetadataPVCTemplate *corev1.PersistentVolumeClaim `json:"metadataPVCTemplate,omitempty"`
	WalPVCTemplate      *corev1.PersistentVolumeClaim `json:"walPVCTemplate,omitempty"`

	// Whether to encrypt the deviceSet or not
	// +optional
	Encrypted *bool `json:"encrypted,omitempty"`
}

// TODO: Fill in the members when the actual configurable options are defined in rook-ceph

// StorageDeviceSetConfig defines Ceph OSD specific config options for the StorageDeviceSet
type StorageDeviceSetConfig struct {
	// TuneSlowDeviceClass tunes the OSD when running on a slow Device Class
	// +optional
	TuneSlowDeviceClass bool `json:"tuneSlowDeviceClass,omitempty"`

	// TuneFastDeviceClass tunes the OSD when running on a fast Device Class
	// +optional
	TuneFastDeviceClass bool `json:"tuneFastDeviceClass,omitempty"`
}

// MultiCloudGatewaySpec defines specific multi-cloud gateway configuration options
type MultiCloudGatewaySpec struct {
	// ReconcileStrategy specifies whether to reconcile NooBaa CRs. Valid
	// values are "manage", "standalone", "ignore" (same as "standalone"),
	// and "" (same as "manage").
	ReconcileStrategy string `json:"reconcileStrategy,omitempty"`

	// DbStorageClassName specifies the default storage class
	// for nooba-db pods
	// +optional
	DbStorageClassName string `json:"dbStorageClassName,omitempty"`
	// Endpoints (optional) sets configuration info for the noobaa endpoint
	// deployment.
	// +optional
	Endpoints *nbv1.EndpointsSpec `json:"endpoints,omitempty"`

	// DisableLoadBalancerService (optional) sets the service type to ClusterIP instead of LoadBalancer
	// +nullable
	// +optional
	DisableLoadBalancerService bool `json:"disableLoadBalancerService,omitempty"`

	// Allows Noobaa to connect to an external Postgres server
	// +optional
	ExternalPgConfig *ExternalPGSpec `json:"externalPgConfig,omitempty"`

	// DenyHTTP (optional) if given will deny access to the NooBaa S3 service using HTTP (only HTTPS)
	// +optional
	DenyHTTP bool `json:"denyHTTP,omitempty"`
}

type ExternalPGSpec struct {
	// PGSecret stores the secret name which contains connection string of the Postgres server
	// +optional
	PGSecretName string `json:"pgSecretName,omitempty"`
	// AllowSelfSignedCerts will allow the Postgres server to use self signed certificates to authenticate
	// +optional
	AllowSelfSignedCerts bool `json:"allowSelfSignedCerts,omitempty"`
	// EnableTLS will allow the postgres server to connect via TLS/SSL
	// +optional
	EnableTLS bool `json:"enableTls,omitempty"`
	// TLSSecret stores the secret name which contains the client side certificates if enabled
	// +optional
	TLSSecretName string `json:"tlsSecretName,omitempty"`
}

// NFSSpec defines specific nfs configuration options
type NFSSpec struct {
	// Enable specifies whether to enable NFS.
	// +optional
	Enable bool `json:"enable,omitempty"`
	// StorageClassName specifies the name of the storage class created for NFS
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$
	StorageClassName string `json:"storageClassName,omitempty"`
	// LogLevel set logging level
	// Log levels: NIV_NULL | NIV_FATAL | NIV_MAJ | NIV_CRIT | NIV_WARN | NIV_EVENT | NIV_INFO | NIV_DEBUG | NIV_MID_DEBUG | NIV_FULL_DEBUG | NB_LOG_LEVEL
	// +optional
	LogLevel          string `json:"logLevel,omitempty"`
	ReconcileStrategy string `json:"reconcileStrategy,omitempty"`
}

// MonitoringSpec controls the configuration of resources for exposing OCS metrics
type MonitoringSpec struct {
	ReconcileStrategy string `json:"reconcileStrategy,omitempty"`
	// Labels to add to monitoring resources created by operator.
	// These labels are used as LabelSelector for Prometheus
	Labels map[string]string `json:"labels,omitempty"`
}

// EncryptionSpec defines if encryption should be enabled for the Storage Cluster
// It is optional and defaults to false.
type EncryptionSpec struct {
	// deprecated from OCS 4.10 onwards, acting as a dummy,
	// UI will keep sending this flag for backward compatibility (OCP 4.10 + OCS 4.9)
	// +optional
	Enable bool `json:"enable,omitempty"`
	// +optional
	ClusterWide bool `json:"clusterWide,omitempty"`
	// +optional
	StorageClass bool `json:"storageClass,omitempty"`
	// StorageClassName specifies the name of the storage class created for ceph encrypted block pools
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$
	StorageClassName     string                   `json:"storageClassName,omitempty"`
	KeyManagementService KeyManagementServiceSpec `json:"kms,omitempty"`
	// KeyRotation defines options for Key Rotation.
	// +optional
	KeyRotation KeyRotationSpec `json:"keyRotation,omitempty"`
}

// KeyRotationSpec represents the settings for Key Rotation.
type KeyRotationSpec struct {
	// Enable represents whether the key rotation is enabled.
	// +optional
	Enable *bool `json:"enable,omitempty"`
	// Schedule represents the cron schedule for key rotation.
	// +optional
	// +kubebuilder:default="@weekly"
	Schedule string `json:"schedule,omitempty"`
}

type MirroringSpec struct {
	// If true, data mirroring is enabled for the StorageCluster.
	// This configuration will only be applied to resources (such as CephBlockPool)
	// managed by the operator.
	// It is optional and defaults to false.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// PeerSecretNames represents the Kubernetes Secret names of rbd-mirror peers tokens
	// +optional
	PeerSecretNames []string `json:"peerSecretNames,omitempty"`
}

// KMSServerConnectionStatus defines the observed connection state to the KMS
// server.
type KMSServerConnectionStatus struct {
	KMSServerAddress         string `json:"kmsServerAddress,omitempty"`
	KMSServerConnectionError string `json:"kmsServerConnectionError,omitempty"`
}

// StorageClusterStatus defines the observed state of StorageCluster
type StorageClusterStatus struct {
	// Version specifies the version of StorageCluster
	Version string `json:"version,omitempty"`

	// Phase describes the Phase of StorageCluster
	// This is used by OLM UI to provide status information
	// to the user
	Phase string `json:"phase,omitempty"`

	// Conditions describes the state of the StorageCluster resource.
	// +optional
	Conditions []conditionsv1.Condition `json:"conditions,omitempty"`

	// RelatedObjects is a list of objects created and maintained by this
	// operator. Object references will be added to this list after they have
	// been created AND found in the cluster.
	// +optional
	RelatedObjects []corev1.ObjectReference `json:"relatedObjects,omitempty"`

	// NodeTopologies is a list of topology labels on all nodes matching
	// the StorageCluster's placement selector.
	// +optional
	NodeTopologies *NodeTopologyMap `json:"nodeTopologies,omitempty"`

	// FailureDomain is the base CRUSH element Ceph will use to distribute
	// its data replicas for the default CephBlockPool
	// +optional
	FailureDomain string `json:"failureDomain,omitempty"`

	// FailureDomainKey is the specific key used to find the locations available
	// under a failure domain. For example topology.kubernetes.io/zone
	// +optional
	FailureDomainKey string `json:"failureDomainKey,omitempty"`

	// FailureDomainValues is the list of locations available for a failure
	// domain under the failure domain key.
	// +optional
	FailureDomainValues []string `json:"failureDomainValues,omitempty"`

	// LastAppliedResourceProfile is the resource profile that was last applied successfully & is currently in use.
	LastAppliedResourceProfile string `json:"lastAppliedResourceProfile,omitempty"`

	// StorageProviderEndpoint holds endpoint info on Provider cluster which is required
	// for consumer to establish connection with the storage providing cluster.
	StorageProviderEndpoint string `json:"storageProviderEndpoint,omitempty"`

	// ExternalSecretHash holds the checksum value of external secret data.
	ExternalSecretHash string `json:"externalSecretHash,omitempty"`

	// Images holds the image reconcile status for all images reconciled by the operator
	Images ImagesStatus `json:"images,omitempty"`

	// DefaultCephDeviceClass holds the default ceph device class to be used for the pools
	DefaultCephDeviceClass string `json:"defaultCephDeviceClass,omitempty"`

	// KMSServerConnection holds the connection state to the KMS server.
	KMSServerConnection KMSServerConnectionStatus `json:"kmsServerConnection,omitempty"`

	// CurrentMonCount holds the value of ceph mons configured in ceph cluster.
	CurrentMonCount int `json:"currentMonCount,omitempty"`
}

// ImagesStatus maps every component image name it's reconciliation status information
type ImagesStatus struct {
	Ceph       *ComponentImageStatus `json:"ceph,omitempty"`
	NooBaaCore *ComponentImageStatus `json:"noobaaCore,omitempty"`
	NooBaaDB   *ComponentImageStatus `json:"noobaaDB,omitempty"`
}

// ComponentImageStatus holds image status information for a specific component image
type ComponentImageStatus struct {
	DesiredImage string `json:"desiredImage,omitempty"`
	ActualImage  string `json:"actualImage,omitempty"`
}

// TopologyLabelValues is a list of values for a topology label
type TopologyLabelValues []string

// NodeTopologyMap represents the list of all values of all topology labels
// across all nodes in the StorageCluster
type NodeTopologyMap struct {
	// Labels is a map of topology label keys
	// (e.g. "failure-domain.kubernetes.io") to a set of values for those
	// keys.
	// +optional
	// +nullable
	Labels map[string]TopologyLabelValues `json:"labels,omitempty"`

	// TODO: Move the failureDomain from the status section to here
	// FailureDomain string `json:"failureDomain,omitempty"`

	// ArbiterLocation is the chosen location in the failure domain for placing the arbiter resources.
	// When the failure domain is not provided as an input, ocs-operator determines the failure domain.
	ArbiterLocation string `json:"arbiterLocation,omitempty"`
}

const (
	// ConditionReconcileComplete communicates the status of the StorageCluster resource's
	// reconcile functionality. Basically, is the Reconcile function running to completion.
	ConditionReconcileComplete conditionsv1.ConditionType = "ReconcileComplete"

	// ConditionExternalClusterConnected condition type indicates
	// the successful connection to an external cluster
	ConditionExternalClusterConnected conditionsv1.ConditionType = "ExternalClusterConnected"

	// ConditionExternalClusterConnecting type indicates that rook is still trying for
	// an external connection
	ConditionExternalClusterConnecting conditionsv1.ConditionType = "ExternalClusterConnecting"

	// ConditionVersionMismatch type indicates that there is a mismatch in the storagecluster
	// and the operator version
	ConditionVersionMismatch conditionsv1.ConditionType = "VersionMismatch"
)

// List of constants to show different different reconciliation messages and statuses.
const (
	ReconcileFailed                 = "ReconcileFailed"
	ReconcileInit                   = "Init"
	ReconcileCompleted              = "ReconcileCompleted"
	ReconcileCompletedMessage       = "Reconcile completed successfully"
	ExternalClusterConnected        = "ExternalClusterConnected"
	ExternalClusterConnectedMessage = "Connected successfully to an external cluster"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=.status.phase,description="Current Phase"
// +kubebuilder:printcolumn:name="External",type=boolean,JSONPath=.spec.externalStorage.enable,description="External Storage Cluster"
// +kubebuilder:printcolumn:name="Created At",type=string,JSONPath=.metadata.creationTimestamp
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=.status.version,description="Storage Cluster Version"
// +operator-sdk:csv:customresourcedefinitions:displayName="Storage Cluster",resources={{CephCluster,v1,cephclusters.ceph.rook.io},{NooBaa,v1alpha1,noobaas.noobaa.io}}

// StorageCluster represents a cluster including Ceph Cluster, NooBaa and all the storage and compute resources required.
type StorageCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StorageClusterSpec   `json:"spec,omitempty"`
	Status StorageClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// StorageClusterList contains a list of StorageCluster
type StorageClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StorageCluster `json:"items"`
}

// ArbiterSpec defines if arbiter should be enabled for the Ceph Cluster.
// It is optional and defaults to false.
// If set to true, ArbiterLocation must be set in the NodeTopologies.
type ArbiterSpec struct {
	Enable bool `json:"enable,omitempty"`
	// DisableMasterNodeToleration can be used to turn off the arbiter mon toleration for the master node taint.
	DisableMasterNodeToleration bool                          `json:"disableMasterNodeToleration,omitempty"`
	ArbiterMonPVCTemplate       *corev1.PersistentVolumeClaim `json:"arbiterMonPVCTemplate,omitempty"`
}

// OverprovisionControlSpec defines the allowed overprovisioning PVC consumption from the underlying cluster.
// This may be an absolute value or as a percentage of the overall effective capacity.
// One, and only one of those two (Capacity and Percentage) may be defined.
type OverprovisionControlSpec struct {
	StorageClassName string                               `json:"storageClassName,omitempty"`
	QuotaName        string                               `json:"quotaName,omitempty"`
	Capacity         resource.Quantity                    `json:"capacity,omitempty"`
	Selector         quotav1.ClusterResourceQuotaSelector `json:"selector,omitempty"`
}

func (r *StorageCluster) NewToolsDeployment(tolerations []corev1.Toleration, nodeAffinity *corev1.NodeAffinity) *appsv1.Deployment {

	var replicaOne int32 = 1

	name := "rook-ceph-tools"
	namespace := r.ObjectMeta.Namespace
	rookImage := os.Getenv("ROOK_CEPH_IMAGE")
	runAsNonRoot := true
	var runAsUser, runAsGroup int64 = 2016, 2016
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicaOne,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "rook-ceph-tools",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "rook-ceph-tools",
					},
				},
				Spec: corev1.PodSpec{
					DNSPolicy:          corev1.DNSClusterFirstWithHostNet,
					ServiceAccountName: "rook-ceph-default",
					Containers: []corev1.Container{
						{
							Name:    name,
							Image:   rookImage,
							Command: []string{"/bin/bash"},
							Args: []string{
								"-m",
								"-c",
								"/usr/local/bin/toolbox.sh",
							},
							TTY: true,
							Env: []corev1.EnvVar{
								{
									Name: "ROOK_CEPH_USERNAME",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon"},
											Key:                  "ceph-username",
										},
									},
								},
								{
									Name: "ROOK_CEPH_SECRET",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon"},
											Key:                  "ceph-secret",
										},
									},
								},
							},
							SecurityContext: &corev1.SecurityContext{
								RunAsNonRoot: &runAsNonRoot,
								RunAsUser:    &runAsUser,
								RunAsGroup:   &runAsGroup,
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "ceph-config", MountPath: "/etc/ceph"},
								{Name: "mon-endpoint-volume", MountPath: "/etc/rook"},
							},
						},
					},
					Tolerations: tolerations,
					Affinity: &corev1.Affinity{
						NodeAffinity: nodeAffinity,
					},
					Volumes: []corev1.Volume{
						{Name: "ceph-config", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						{Name: "mon-endpoint-volume", VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon-endpoints"},
								Items: []corev1.KeyToPath{
									{Key: "data", Path: "mon-endpoints"},
								},
							},
						},
						},
					},
				},
			},
		},
	}
}
