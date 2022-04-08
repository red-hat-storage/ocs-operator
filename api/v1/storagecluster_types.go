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
	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	quotav1 "github.com/openshift/api/quota/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
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
	Resources          map[string]corev1.ResourceRequirements `json:"resources,omitempty"`
	Encryption         EncryptionSpec                         `json:"encryption,omitempty"`
	StorageDeviceSets  []StorageDeviceSet                     `json:"storageDeviceSets,omitempty"`
	MonPVCTemplate     *corev1.PersistentVolumeClaim          `json:"monPVCTemplate,omitempty"`
	MonDataDirHostPath string                                 `json:"monDataDirHostPath,omitempty"`
	MultiCloudGateway  *MultiCloudGatewaySpec                 `json:"multiCloudGateway,omitempty"`
	NFS                *NFSSpec                               `json:"nfs,omitempty"`
	// Monitoring controls the configuration of resources for exposing OCS metrics
	Monitoring *MonitoringSpec `json:"monitoring,omitempty"`
	// Version specifies the version of StorageCluster
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
	Mirroring MirroringSpec `json:"mirroring,omitempty"`
	// OverprovisionControl specifies the allowed hard-limit PVs overprovisioning relative to
	// the effective usable storage capacity.
	OverprovisionControl []OverprovisionControlSpec `json:"overprovisionControl,omitempty"`

	// AllowRemoteStorageConsumers Indicates that the OCS cluster should deploy the needed
	// components to enable connections from remote consumers.
	AllowRemoteStorageConsumers bool `json:"allowRemoteStorageConsumers,omitempty"`
}

// KeyManagementServiceSpec provides a way to enable KMS
type KeyManagementServiceSpec struct {
	// +optional
	Enable bool `json:"enable,omitempty"`
}

// ManagedResourcesSpec defines how to reconcile auxiliary resources
type ManagedResourcesSpec struct {
	CephCluster          ManageCephCluster          `json:"cephCluster,omitempty"`
	CephConfig           ManageCephConfig           `json:"cephConfig,omitempty"`
	CephDashboard        ManageCephDashboard        `json:"cephDashboard,omitempty"`
	CephBlockPools       ManageCephBlockPools       `json:"cephBlockPools,omitempty"`
	CephFilesystems      ManageCephFilesystems      `json:"cephFilesystems,omitempty"`
	CephObjectStores     ManageCephObjectStores     `json:"cephObjectStores,omitempty"`
	CephObjectStoreUsers ManageCephObjectStoreUsers `json:"cephObjectStoreUsers,omitempty"`
}

// ManageCephCluster defines how to reconcile the Ceph cluster definition
type ManageCephCluster struct {
	ReconcileStrategy string `json:"reconcileStrategy,omitempty"`
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

// ManageCephBlockPools defines how to reconcilea CephBlockPools
type ManageCephBlockPools struct {
	ReconcileStrategy    string `json:"reconcileStrategy,omitempty"`
	DisableStorageClass  bool   `json:"disableStorageClass,omitempty"`
	DisableSnapshotClass bool   `json:"disableSnapshotClass,omitempty"`
}

// ManageCephFilesystems defines how to reconcile CephFilesystems
type ManageCephFilesystems struct {
	ReconcileStrategy    string `json:"reconcileStrategy,omitempty"`
	DisableStorageClass  bool   `json:"disableStorageClass,omitempty"`
	DisableSnapshotClass bool   `json:"disableSnapshotClass,omitempty"`
}

// ManageCephObjectStores defines how to reconcile CephObjectStores
type ManageCephObjectStores struct {
	ReconcileStrategy   string `json:"reconcileStrategy,omitempty"`
	DisableStorageClass bool   `json:"disableStorageClass,omitempty"`
	GatewayInstances    int32  `json:"gatewayInstances,omitempty"`
	DisableRoute        bool   `json:"disableRoute,omitempty"`
}

// ManageCephObjectStoreUsers defines how to reconcile CephObjectStoreUsers
type ManageCephObjectStoreUsers struct {
	ReconcileStrategy string `json:"reconcileStrategy,omitempty"`
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

	//+kubebuilder:validation:Enum=ocs;rhcs
	// StorageProviderKind Identify the type of storage provider cluster this consumer cluster is going to connect to.
	StorageProviderKind ExternalStorageKind `json:"storageProviderKind,omitempty"`

	// StorageProviderEndpoint holds info to establish connection with the storage providing cluster.
	StorageProviderEndpoint string `json:"storageProviderEndpoint,omitempty"`

	// OnboardingTicket holds an identity information required for consumer to onboard.
	OnboardingTicket string `json:"onboardingTicket,omitempty"`

	// RequestedCapacity Will define the desired capacity requested by a consumer cluster.
	RequestedCapacity *resource.Quantity `json:"requestedCapacity,omitempty"`
}

// ExternalStorageClusterStatus defines the status of the external Storage Cluster
// to be connected to the local cluster
type ExternalStorageClusterStatus struct {
	// GrantedCapacity Will report the actual capacity
	// granted to the consumer cluster by the provider cluster.
	GrantedCapacity resource.Quantity `json:"grantedCapacity,omitempty"`

	// ConsumerID will hold the identity of this cluster inside the attached provider cluster
	ConsumerID string `json:"id,omitempty"`
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
	// CRUSH map.
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
}

// NFSSpec defines specific nfs configuration options
type NFSSpec struct {
	// Enable specifies whether to enable NFS.
	// +optional
	Enable bool `json:"enable,omitempty"`
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
	StorageClass         bool                     `json:"storageClass,omitempty"`
	KeyManagementService KeyManagementServiceSpec `json:"kms,omitempty"`
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

// StorageClusterStatus defines the observed state of StorageCluster
type StorageClusterStatus struct {
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

	// StorageProviderEndpoint holds endpoint info on Provider cluster which is required
	// for consumer to establish connection with the storage providing cluster.
	StorageProviderEndpoint string `json:"storageProviderEndpoint,omitempty"`

	// ExternalSecretHash holds the checksum value of external secret data.
	ExternalSecretHash string `json:"externalSecretHash,omitempty"`

	// ExternalStorage shows the status of the external cluster
	ExternalStorage ExternalStorageClusterStatus `json:"externalStorage,omitempty"`

	// Images holds the image reconcile status for all images reconciled by the operator
	Images ImagesStatus `json:"images,omitempty"`
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
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=.spec.version,description="Storage Cluster Version"

// StorageCluster is the Schema for the storageclusters API
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

func init() {
	SchemeBuilder.Register(&StorageCluster{}, &StorageClusterList{})
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
