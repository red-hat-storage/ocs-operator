package v1

import (
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	rookalpha "github.com/rook/rook/pkg/apis/rook.io/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file

// StorageClusterSpec defines the desired state of StorageCluster
type StorageClusterSpec struct {
	ManageNodes       bool                          `json:"manageNodes,omitempty"`
	InstanceType      string                        `json:"instanceType,omitempty"`
	StorageDeviceSets []StorageDeviceSet            `json:"storageDeviceSets,omitempty"`
	MonPVCTemplate    *corev1.PersistentVolumeClaim `json:"monPVCTemplate,omitempty"`
}

// StorageDeviceSet defines a set of storage devices.
// It is derived from and mapped to StorageClassDeviceSet in Rook.
type StorageDeviceSet struct {
	Name string `json:"name"`
	// +kubebuilder:validation:Minimum=1
	Count           int                          `json:"count"`
	Resources       corev1.ResourceRequirements  `json:"resources"`
	Placement       rookalpha.Placement          `json:"placement"`
	Config          StorageDeviceSetConfig       `json:"config,omitempty"`
	DataPVCTemplate corev1.PersistentVolumeClaim `json:"dataPVCTemplate"`
	Portable        bool                         `json:"portable"`
}

// StorageDeviceSetConfig defines Ceph OSD specific config options for the StorageDeviceSet
// TODO: Fill in the members when the actual configurable options are defined in rook-ceph
type StorageDeviceSetConfig struct{}

// StorageClusterStatus defines the observed state of StorageCluster
// +k8s:openapi-gen=true
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
}

// ConditionReconcileComplete communicates the status of the StorageCluster resource's
// reconcile functionality. Basically, is the Reconcile function running to completion.
const ConditionReconcileComplete conditionsv1.ConditionType = "ReconcileComplete"

// List of constants to show different different reconciliation messages and statuses.
const (
	ReconcileFailed           = "ReconcileFailed"
	ReconcileInit             = "Init"
	ReconcileCompleted        = "ReconcileCompleted"
	ReconcileCompletedMessage = "Reconcile completed successfully"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// StorageCluster is the Schema for the storageclusters API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type StorageCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StorageClusterSpec   `json:"spec,omitempty"`
	Status StorageClusterStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// StorageClusterList contains a list of StorageCluster
type StorageClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StorageCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&StorageCluster{}, &StorageClusterList{})
}

// ToStorageClassDeviceSet converts a StorageDeviceSet object to a Rook
// StorageClassDeviceSet object
func (ds *StorageDeviceSet) ToStorageClassDeviceSet() rookalpha.StorageClassDeviceSet {
	return rookalpha.StorageClassDeviceSet{
		Name:                 ds.Name,
		Count:                ds.Count,
		Resources:            ds.Resources,
		Placement:            ds.Placement,
		Config:               ds.Config.ToMap(),
		VolumeClaimTemplates: []corev1.PersistentVolumeClaim{ds.DataPVCTemplate},
		Portable:             ds.Portable,
	}
}

// ToMap converts a StorageDeviceSetConfig object to a map[string]string that
// can be set in a Rook StorageClassDeviceSet object
// This functions just returns `nil` right now as the StorageDeviceSetConfig
// struct itself is empty. It will be updated to perform actual conversion and
// return a proper map when the StorageDeviceSetConfig struct is updated.
// TODO: Do actual conversion to map when StorageDeviceSetConfig has defined members
func (c *StorageDeviceSetConfig) ToMap() map[string]string {
	return nil
}
