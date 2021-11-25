# FEATURE: OCS to OCS API changes

## Summary
Allow customers to share OCS storage across multiple OCP clusters. Each
consumer OCP cluster will have External OCS deployed that connects to the
provider cluster.

## Motivation
This feature gives flexibility to customers and lets them share storage and
better utilize their OpenShift clusters.
It is more cost effective, especially for cases when the storage capacity
needed by the OCP cluster is low.

### Goals
Allow a customer to have a single OCS provider cluster sharing the storage
across multiple OCP clusters. Each consumer OCP cluster will have OCS deployed
in external mode that connects to an internal mode provider cluster. Also,
allow us to reduce the cost of storage and maintenance.

### Non-Goals
Networking b/w two OCP clusters.

## Proposal
Introduce additional StorageCluster spec fields to support a new external mode
where the storage is an OCS cluster.

## Design Details

### API changes required for a provider deployment
```yaml
spec:
    allowRemoteStorageConsumers: <bool>
status:
    storageProviderEndpoint: <string>
```

* `spec.allowRemoteStorageConsumers` Indicates that the OCS cluster should
deploy the needed components to enable connections from remote consumers.
When enabled the operator will deploy an API server that will allow onboarding,
offboarding and second-day operation requests coming from consumer clusters.

- onboarding: Configure the consumer to use the storage provided by the
provider cluster. The consumer will install the OCS in external mode and
connect to the provider cluster.
- offboarding: Uninstall the ocs cluster and disconnect it from the provider.

* `status.storageProviderEndpoint` It holds endpoint info on Provider cluster
which is required for consumer to establish connection with the storage
providing cluster.

### API changes required for a consumer deployment
```yaml
spec:
    externalStorage:
        enable: <bool> (existing field)
        storageProviderKind: <string> (ocs/rhcs)
        storageProviderEndpoint: <string>
        onboardingTicket: <string>
        requestedCapacity: <resource.Quantity>
status:
    externalStorage:
        grantedCapacity: <resource.Quantity>
        id: <string>
```

* `spec.externalStorage.storageProviderKind` Identify the type of storage
provider cluster this consumer cluster is going to connect to.
  - A value of RHCS will indicate that the consumer cluster is going to connect
  to and consume storage from a **rhcs** deployment (the legacy external mode).
  - A value of **ocs-provider** will indicate that the consumer cluster is
  going to connect to and consume storage from an ocs storage provider cluster.
* `spec.externalStorage.storageProviderEndpoint` It holds the connection
information that is needed in order to establish connection with the storage
providing cluster.
* `spec.externalStorage.onboardingTicket` It holds an identity information
required for consumer to onboard.
* `spec.externalStorage.requestedCapacity` Will define the desired capacity
requested by a consumer cluster deployment.
* `status.externalStorage.grantedCapacity` Will report the actual capacity
granted to the consumer cluster by the provider cluster.
* `status.externalStorage.id` It will hold the identity of this cluster inside
the attached provider cluster.

#### There are be 3 types of operations which will be added to the ocs-operator
* **OnBoarding:** ocs-operator, running on a consumer cluster, will reach out
to an endpoint on the OCS provider cluster and request provisioning of all the
resources (mainly rook resources) needed for supporting storage consumption by
this cluster. The remote OCS cluster will return an external mode configuration
that will allow the ocs operator to configure the underlying rook operator to
connect to the Ceph cluster deployed on the provider cluster.
* **UpScaling/DownScaling:** ocs-operator, running on a consumer cluster, will
reach out to an endpoint on the OCS provider cluster and request to update the
underlying rook resources to allow consumption of additional/less storage.
* **OffBoarding:** ocs-operator, running on a consumer cluster, will reach out
to an endpoint on the OCS provider cluster to indicate that it is no longer
using storage from the provider cluster and allow the provider cluster to clean
up all the rook resources provisioned for that specific consumer cluster.

### Test Plan
Running e2e for this feature requires 2 OCP clusters and network communication
between them to be enabled. Only unit tests will be added for now.

### Monitoring Requirements
None

### Dependencies
None

## Alternatives
None
