package types

// DiscoveredDeviceType is the types that will be discovered by the device finder.
type DiscoveredDeviceType string

const (
	// DiskType represents a device-type of block disk
	DiskType DiscoveredDeviceType = "disk"
	// PartType represents a device-type of partition
	PartType DiscoveredDeviceType = "part"
	// LVMType is an LVM type
	LVMType DiscoveredDeviceType = "lvm"
	// MultiPathType is a multipath type
	MultiPathType DiscoveredDeviceType = "mpath"
)

// DiscoveredDevice shows the list of discovered devices with their properties
type DiscoveredDevice struct {
	// DeviceID represents the persistent name of the device. For eg, /dev/disk/by-id/...
	DeviceID string `json:"deviceID"`
	// Path represents the device path. For eg, /dev/sdb
	Path string `json:"path"`
	// Model of the discovered device
	Model string `json:"model"`
	// Type of the discovered device
	Type DiscoveredDeviceType `json:"type"`
	// Vendor of the discovered device
	Vendor string `json:"vendor"`
	// Size of the discovered device
	Size int64 `json:"size"`
	// WWN defines the WWN value of the device.
	WWN string `json:"WWN"`
}

// DiscoveryResult represents the result of device discovery for a node
type DiscoveryResult struct {
	// DiscoveredDevices contains the list of devices which are usable
	// for creating LocalDisks
	// The devices in this list qualify these following conditions.
	// - it should be a non-removable device.
	// - it should not be a read-only device.
	// - it should not be mounted anywhere
	// - it should not be a boot device
	// - it should not have child partitions
	// - it should have a WWN value
	// +optional
	DiscoveredDevices []DiscoveredDevice `json:"discoveredDevices"`
}
