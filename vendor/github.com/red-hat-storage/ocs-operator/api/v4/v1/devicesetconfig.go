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

// ToMap converts a StorageDeviceSetConfig object to a map[string]string that
// can be set in a Rook StorageClassDeviceSet object
// This functions just returns `nil` right now as the StorageDeviceSetConfig
// struct itself is empty. It will be updated to perform actual conversion and
// return a proper map when the StorageDeviceSetConfig struct is updated.
// TODO: Do actual conversion to map when StorageDeviceSetConfig has defined members
func (c *StorageDeviceSetConfig) ToMap() map[string]string {
	return nil
}
