// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1

// AllowedFlexVolumeApplyConfiguration represents a declarative configuration of the AllowedFlexVolume type for use
// with apply.
type AllowedFlexVolumeApplyConfiguration struct {
	Driver *string `json:"driver,omitempty"`
}

// AllowedFlexVolumeApplyConfiguration constructs a declarative configuration of the AllowedFlexVolume type for use with
// apply.
func AllowedFlexVolume() *AllowedFlexVolumeApplyConfiguration {
	return &AllowedFlexVolumeApplyConfiguration{}
}

// WithDriver sets the Driver field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Driver field is set to the value of the last call.
func (b *AllowedFlexVolumeApplyConfiguration) WithDriver(value string) *AllowedFlexVolumeApplyConfiguration {
	b.Driver = &value
	return b
}
