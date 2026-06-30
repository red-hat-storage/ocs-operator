module github.com/red-hat-storage/ocs-operator/services/provider/api/v4

go 1.22.7

require (
	google.golang.org/grpc v1.68.0
	google.golang.org/protobuf v1.36.4
)

require (
	golang.org/x/net v0.34.0 // indirect
	golang.org/x/sys v0.29.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250115164207-1a7da9e5054f // indirect
)

replace google.golang.org/grpc => github.com/openshift-sustaining/grpc-go v1.71.3-sec.1
