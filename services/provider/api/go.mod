module github.com/red-hat-storage/ocs-operator/services/provider/api/v4

go 1.22.7

require (
	google.golang.org/grpc v1.68.0
	google.golang.org/protobuf v1.36.4
)

require (
	golang.org/x/net v0.34.0 // indirect
	golang.org/x/sys v0.30.0 // indirect
	golang.org/x/text v0.22.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250115164207-1a7da9e5054f // indirect
)

// Replace dependencies with OpenShift-sustaining versions to fix CVEs without bumping Go Minor version
replace (
	golang.org/x/net => github.com/openshift-sustaining/net v0.35.0-sec.1
	google.golang.org/grpc => github.com/openshift-sustaining/grpc-go v1.71.3-sec.1
)
