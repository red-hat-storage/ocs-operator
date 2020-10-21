package v1

// ToMap converts a StorageDeviceSetConfig object to a map[string]string that
// can be set in a Rook StorageClassDeviceSet object
// This functions just returns `nil` right now as the StorageDeviceSetConfig
// struct itself is empty. It will be updated to perform actual conversion and
// return a proper map when the StorageDeviceSetConfig struct is updated.
// TODO: Do actual conversion to map when StorageDeviceSetConfig has defined members
func (c *StorageDeviceSetConfig) ToMap() map[string]string {
	return nil
}
