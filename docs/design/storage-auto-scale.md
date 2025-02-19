# Storage Automatic Scaler

# Summary

This feature would help users to get rid of maintenance task of keeping the system with enough available free space.
In virtualized environments, we can scale the storage devices OSDs so this feature helps in automatic scaling. 

# Proposal

Implement auto storage scale CR per device class of a specific Storagecluster.

# High-Level Design

- Create a StorageAutoScale CR per Storagecluster device class
    ```
    apiVersion: ocs.openshift.io/v1
    Kind: StorageAutoScaling
    Metadata:
        name: <storageclusterName>-<deviceClassName>
        namespace: openshift-storage
    Spec:
        storageClusterName: <storageclusterName>
        deviceClassName: <deviceClassName>
        capacityLimit: <capacity>   
    Status:
        phase: (waiting/inProgress/completed/failure)
        LastRunTimeStamp: (only on completed/failure)
        currentOSDSize: 2TiB
        expectedOSDSize: 4TiB
        currentOSDCount: 3​
        expectedOSDCount: 3
    ```

- Watch prometheus periodically after 10 mins and depending on the OSD fill percentage, reconcile the StorageAutoScaling CR.
  
- If the OSD size is less than 8Tib, do vertical scaling by doubling the each osd sizes for that device class.

- If the OSD size is equal to 8Tib, do a horizontal scaling, by adding 1 osd of 8Tib on each storage node, by simply increasing the (count+1) in the device set associated with a device class.   

# Low-Level Design 

1) Add a single go-routine to watch periodically(10mins) all the OSDs sizes using Prometheus metrics in the storagecluster namespace.
   1) With every run calculate different device class highest osd filled percentage.
       `OsdPercentage`:
        ```
        ssd               69%
        nvme               71%
        ...
        ```
    2) If the highest osd percentage reported is more than 73-74%(reaching osd nearfull), scaling is needed.
    3) If scaling is needed calculate the `expectedOSDSize` and `expectedOSDCount` per device class.
       1)  Calculate the total number osds and osd size per device class from the prometheus response.
       2)  Calculate the total number osds and osd size per device class from the storageCluster.
       3)  If the above values are equal(needed to avoid double re-sizing), create or update the sync-map mapping:
           1)  If the OSD size is less than 8Tib, do vertical scaling by doubling the each osd sizes for that device class.
           2)  If the OSD size is equal to 8Tib, do a horizontal scaling, by adding 1 osd of 8Tib on each storage node.
  
    4) Go routine would update the [sync-map](https://go.dev/src/sync/map.go) with different device class expected state.

        `deviceClassScaling` sync-map:

        ```
        ssd:
            expectedOSDSize: 4
            expectedOSDCount: 6
        hdd:
            expectedOSDSize: 4
            expectedOSDCount: 9
        ...
        ```

2) If the sync map is created or updated then go routine will send a signal to the go channel.

3) Create a named controller that watches for [channel generic event](https://book-v1.book.kubebuilder.io/beyond_basics/controller_watches).

4) If there is an update on the channel, watcher will reconcile the new named controller.
   1) Named Controller will list all the StorageAutoScaling CRs.
   2) Depending on the `OsdPercentage sync-map` it will set the `expectedOSDSize` and `expectedOSDCount` and on the status and update `phase` to `in-progress` on the `StorageAutoScaling` CR which need scaling.
   3) Check if the totalCapacity allows to scale it or not.
   4) Patch the Storagecluster, with all the device class device sets update needed at the same time.
   5) Verify the Storagecluster whether the new osds are added or scaled in size, for all the device class device sets. 
   6) If the scaling is successful will raise a `success` alert and update the status of all the `StorageAutoScaling` CR.
   7) If it is fails will do a re-reconcile and, if 3 recent reconcile fails(track by observed generation) will raise a `failure` alert.   
 
5) Sync map will act as the desired state and source of truth for the controller.

Note:
    The above prometheus query work per namespace, so the algorithm will be per storagecluster operation, later in future if we need the auto scaling for different storagecluster, either run multiple go-routine and multiple sync-maps, 
    Or update the current prometheus query and sync-map to accommodate different storagecluster values.

# Failure case

There might cases where system resources are exhausted and new OSDs cannot be started.

