# OCS to OCSs : API Server

## Introduction
This document defines the API server used for communication between OCS provider and OCS consumer clusters.

## RPC Interface

#### OCSProvider service

``` protobuf
syntax = "proto3";
package provider;

import "google/protobuf/descriptor.proto";
import "k8s.io/apimachinery/pkg/api/resource/generated.proto";

option go_package = "./;providerpb";


// OCSProvider holds the RPC methods that the OCS consumer can use to communicate with remote OCS provider cluster
service OCSProvider {
  // OnBoardConsumer RPC call to validate the consumer and create StorageConsumer
  // resource on the storage provider cluster
  rpc OnBoardConsumer (OnBoardConsumerRequest)
  returns (OnBoardConsumerResponse) {}
  // GetStorageConfig RPC call to generate the json config for connecting to storage provider cluster
  rpc GetStorageConfig(StorageConfigRequest)
  returns (StorageConfigResponse) {}
  // UpdateCapacity RPC call to increase or decrease the storage pool size
  rpc UpdateCapacity(UpdateCapacityRequest)
  returns (UpdateCapacityResponse) {}
  // OffboardConsumer RPC call to delete StorageConsumer CR on the storage provider cluster.
  rpc OffboardConsumer (OffboardConsumerRequest)
  returns (OffboardConsumerResponse) {}
}
```

#### OnboardConsumer
- Check if any existing `StorageConsumer` CR with same token is available and return its encrypted UUID
- If no existing `StorageConsumer` CR found:
    - Create `StorageConsumer` CR after validating the token.
    - Return the encrypted UUID of the `StorageConsumer` CR

```protobuf
// OnBoardConsumerRequest holds the required information to validate the consumer and create StorageConsumer
// resource on the StorageProvider cluster
message OnBoardConsumerRequest{
    // token provided by the provider cluster admin to authenticate the consumer
    string token =1;
    // capacity is the desired storage requested by the consumer cluster
    k8s.io.apimachinery.pkg.api.resource.Quantity capacity = 2;
}

// OnBoardConsumerResponse holds the response for OnBoardConsumer API request
message OnBoardConsumerResponse{
    // K8s UID (UUID) of the consumer cluster
    string storageConsumerUUID =1;
    // grantedCapacity is the storage granted by the provider cluster
    k8s.io.apimachinery.pkg.api.resource.Quantity grantedCapacity = 2;
}

```

##### Response Scheme


| Condition | gRPC Code | Description | Recovery |
| -------- | -------- | -------- | -------- |
| Success    | 0  OK     | Request Successful. StorageConsumer CR is created    | N/A |
| Invalid ID | 3 INVALID_ARGUMENT | Invalid request token or token expired | Contact the Provider Cluster Admin to verify the token
| Error | 13 INTERNAL | Request failed due to some error in provider cluster. For example: failed to create the StorageConsumer CR| Contact Provider Cluster Admin


#### GetStorageConfig
- Read the `status.state` of the StorageConsumer CR:
  - if `status` is `Failed`, return error.
  - if `status` is `Configuring`, return `UNAVAILABLE`.
  - if `status` is `Ready`, fetch the `blockPoolName` and the`cephUser` details from the CR status. Generate and return json connection details.

**Note**: When `UNAVAILABLE` status is returned, the consumer controller should requeue (with interval) the reconciler to call the `GetProviderConfig` API again to check if the rook resources are available in the `StorageConsumer` CR status.

```protobuf
// StorageConfigRequest holds the information required generate the json config for connecting to storage provider cluster
message StorageConfigRequest{
   // K8s UID (UUID) of the consumer cluster
    string storageConsumerUUID =1;
}

// StorageConfigResponse holds the response for the GetStorageConfig API request
message StorageConfigResponse{
    // data contains the json blob to be used by the consumer cluster to connect with the provider cluster
   bytes data = 1;
}

```
##### Response Scheme


| Condition | gRPC Code | Description | Recovery |
| -------- | -------- | -------- | -------- |
| Success    | 0  OK     | Request Successful. StorageConsumer CR is `Ready`, rook resources are created and external python script was executed successfully to generate the json connection blob.    | N/A |
| Awaiting Ceph resources    | 14 UNAVAILABLE     | Waiting for the ceph resources to be created and updated in the StoragConsumer CR status    | N/A |
| Invalid ID | 3 INVALID_ARGUMENT | Request failed due to invalid StorageConsumer ID | Contact the Provider Cluster Admin to verify the StorageConsumer ID
| Error | 13 INTERNAL | Request failed due to some error in provider cluster | Contact Provider Cluster Admin

#### OffboardConsumer
- Make a delete request on the StorageConsumer CR and return OK.

```protobuf
// OffBoardConsumerRequest holds the required information to delete the StorageConsumer CR on the storage provider cluster
message OffBoardConsumerRequest{
    // K8s UID (UUID) of the consumer cluster
    string storageConsumerUUID =1;
}

// OffBoardConsumerResponse holds the response for the OffBoardConsumer API request
message OffBoardConsumerResponse{

}

```

##### Response Scheme
| Condition | gRPC Code | Description | Recovery |
| -------- | -------- | -------- | -------- |
| Success    | 0  OK     | Request Successful     | N/A |
| Invalid ID | 3 INVALID_ARGUMENT | Request failed due to invalid StorageConsumer ID | Contact the Provider Cluster Admin to verify the ID
| Error | 13 INTERNAL | Request failed due to some error in provider cluster | Contact Provider Cluster Admin



#### UpdateCapacity
- Read the `status.state` of the StorageConsumer CR:
  - if Status is `Ready`, then update the `spec.Capacity` and return OK

```protobuf
// UpdateCapacityRequest holds the information required to increase or decrease the block pool size
// on the provider cluster
message UpdateCapacityRequest{
    // K8s UID (UUID) of the consumer cluster
    string storageConsumerUUID =1;
    // capacity is the desired storage requested by the consumer cluster
    k8s.io.apimachinery.pkg.api.resource.Quantity capacity = 2;
}

// UpdateCapacityResponse holds the response for UpdateCapacity API request
message UpdateCapacityResponse{
    // grantedCapacity is the storage granted by the provider cluster
    k8s.io.apimachinery.pkg.api.resource.Quantity grantedCapacity = 2;
}
```

##### Response Scheme
| Condition | gRPC Code | Description | Recovery |
| -------- | -------- | -------- | -------- |
| Success    | 0  OK     | Request Successful     | N/A |
| Invalid  | 3 INVALID_ARGUMENT | Request failed to invalid Consumer ID or Capacity | Contact the Provider Cluster Admin
| Error | 13 INTERNAL | Request failed due to some error in the Provider Cluster | Contact Provider Cluster Admin



## PROJECT STRUCTURE
- The API server would be part of the OCS Operator.
- It would be deployed as a separate Deployment only after user has enabled it in the StorageCluster CR via `spec.allowRemoteStorageConsumers`

```
.
├── services
    ├── provider
        ├── pb
            ├── provider.proto
            ├── provider.pb.go
            ├── README.md
        ├── client
            ├── client.go
        ├── server
            ├── server.go

```

## API SECURITY
#### TODO


## Alternatives
Other than gRPC, REST API can also be used to create this service.

### gRPC vs REST

- #### Performance:
    - gRPC is more performant than REST due to binary payloads.
    - gRPC comes with HTTP2 by default. This has additional benefits like smaller packet size due to compressed headers, multiplexing (same TCP connection for multiple requests), server push streams, etc.
    - Current use case does not involve frequent requests between client and server. So performance aspect of the gRPC can be ignored.

- #### Security:
    - Both provide security with TLS support.
    - gRPC endpoint can't be invoked via browser/postman.

We chose gRPC as we are on k8s and gRPC has more advantages compared to REST
