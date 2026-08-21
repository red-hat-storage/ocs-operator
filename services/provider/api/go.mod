module github.com/red-hat-storage/ocs-operator/services/provider/api/v4

go 1.23.0

toolchain go1.23.10

require (
	google.golang.org/grpc v1.68.0
	google.golang.org/protobuf v1.36.6
)

require (
	golang.org/x/net v0.41.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/text v0.26.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250707201910-8d1bb00bc6a7 // indirect
)

replace google.golang.org/grpc => github.com/openshift-sustaining/grpc-go v1.75.1-sec.1
