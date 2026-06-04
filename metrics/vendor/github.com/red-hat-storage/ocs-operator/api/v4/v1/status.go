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

// These constants represent the overall Phase as used by .Status.Phase
var (
	// PhaseIgnored is used when a resource is ignored
	PhaseIgnored = "Ignored"
	// PhaseProgressing is used when SetProgressingCondition is called
	PhaseProgressing = "Progressing"
	// PhaseError is used when SetErrorCondition is called
	PhaseError = "Error"
	// PhaseReady is used when SetCompleteCondition is called
	PhaseReady = "Ready"
	// PhaseNotReady is used when waiting for system to be ready
	// after reconcile is successful
	PhaseNotReady = "Not Ready"
	// PhaseClusterExpanding is used when cluster is expanding capacity
	PhaseClusterExpanding = "Expanding Capacity"
	// PhaseDeleting is used when cluster is deleting
	PhaseDeleting = "Deleting"
	// PhaseConnecting is used when cluster is connecting to external cluster
	PhaseConnecting = "Connecting"
	// PhaseOnboarding is used when consumer is Onboarding
	PhaseOnboarding = "Onboarding"
)
