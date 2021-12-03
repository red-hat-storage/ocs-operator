# OCS to OCSs : StorageConsumer CRD

## Introduction
This document describes the design of StorageConsumer API (and accompanying controller) representing a consumer cluster inside the provider cluster. This API is an abstraction on top of lower level rook resources that need to be defined in order to provision storage for use by a specific consumer (remote StorageCluster) and acts as a source of truth for consumer cluster requests/configuration. 

## Design Details

### API design of StorageConsumer
```yaml
apiVersion: ocs.openshift.io/v1alpha1
kind: StorageConsumer
metadata:
  name: consumer-1
  finalizers:
    - <string>
  namespace: openshift-storage
spec:
  capacity: <resource.Quantity>
status:
  state: <string>
  grantedCapacity: <resource.Quantity>
  cephObjects:  
    blockPoolName: <string>
    cephUser: <string>
  connectionDetails: <*v1.SecretKeySelector>
```

* `spec.capacity` Will define the desired capacity requested by a consumer cluster deployment.

* `status.grantedCapacity` Will report the capacity granted to the consumer cluster by the provider cluster

* `status.state` Will report the state of the StorageConsumer. Available states are Ready, Configuring, Deleting, Failed. 

* `status.cephObjects` Will report the created ceph objects required for external storage.
  * `status.cephObjects.blockPoolName` Will report the name of created ceph block pool
  * `status.cephObjects.cephUser` Will report the name of created ceph user

* `status.connectionDetails` Will hold the reference to secret containing external connection details (blob).
  * // TODO: Define structure of the secret that is referenced 

### Design of StorageConsumer controller

#### StorageConsumer Operations
* **OnBoarding:** On creation of a StorageConsumer CR, the controller sets a finalizer on the CR. The `spec.capacity` field is specified during the onboarding request by the consumer. Based on this value, the controller creates the relevant Rook-Ceph resources with quota. When all the Rook-Ceph resources are ready, the controller updates the `status.grantedCapacity` and `status.cephObjects`. Finally, the controller will generate a JSON blob containing the external connection details (which will be provided to the consumer cluster) and store it in a Secret with a deterministic name. The `status.connectionDetails` is updated with reference to the created Secret.

* **UpScaling/DownScaling:** When the `spec.capacity` field is modified, the controller reconciles by modifying the quotas of the underlying Rook-Ceph resources.

* **OffBoarding:** When a StorageConsumer CR is deleted, The controller removes the underlying Rook-Ceph resources. Once all resources are removed, the controller removes the finalizer on the StorageConsumer CR to allow it to finish being deleted. 