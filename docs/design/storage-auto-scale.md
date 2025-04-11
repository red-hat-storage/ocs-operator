# Storage Automatic Scaler

## Summary

This feature would help users to get rid of maintenance task of keeping the system with enough available free space.
In virtualized environments, we can scale the storage devices Osds so this feature helps in automatic scaling. 

## Proposal

Implement auto storage scale CR per device class of a specific Storagecluster.

## High-Level Design

- Create a StorageAutoScale CR per Storagecluster device class.
- UI will create the StorageAutoScale CR with a unique name i.e. <storageclusterName>-<deviceClassName>.
  
    ```
    apiVersion: ocs.openshift.io/v1
    kind: StorageAutoScaler
    metadata:
        name: <name>
        namespace: openshift-storage
    spec:
        storageCluster: 
            name: <storageclusterName>
        deviceClass: ssd
        storageCapacityLimit: <capacity>
        storageScalingThresholdPercent: 70  # should be less than osd nearfull percentage 
        maxOsdSize: 8Ti
        timeoutSeconds: 1800   
    status:
        phase: ("" | NotStarted | InProgress | Succeeded | Failed)
        error:
            message: ""
            timestamp: <time>
        storageCapacityLimitReached: false
        lastExpansion:
            startTime: (always set after a new expansion is started)
            completionTime: (only set on success)
            startOsdSize: 2TiB
            expectedOsdSize: 4TiB
            startOsdCount: 3​
            expectedOsdCount: 3
            startStorageCapacity: 6TiB
            expectedStorageCapacity: 12TiB
    ```

- Watch prometheus periodically every 10 mins and depending on the Osd fill percentage, reconcile the StorageAutoScaling CR.
  
- If the Osd size is less than maxOsdSize(default:8Tib), do vertical scaling by doubling the each osd sizes for that device class.

- If the Osd size is equal to maxOsdSize(default:8Tib), do a horizontal scaling, by adding 1 osd of 8Tib on each storage node, by simply increasing the (count+1) in the device set associated with a device class.   

## Low-Level Design 

Go-routine Scraper:

1) Add a single go-routine to watch periodically(10mins) all the OSDs sizes using Prometheus metrics in the storagecluster namespace.
    Query: (ceph_osd_metadata * on (ceph_daemon, namespace, managedBy) group_right(device_class,hostname) (ceph_osd_stat_bytes_used / ceph_osd_stat_bytes))

2) With every run calculate different device class highest osd filled percentage.

        OsdPercentage:
        
        ssd               69%
        nvme              71%
        ...   

3) Create a sync map with `OsdPercentage`, Send a event to the go channel.

Controller:

1) Create a named controller that watches for [channel generic event](https://book-v1.book.kubebuilder.io/beyond_basics/controller_watches) per device class.

2) Set the status `phase` to `NotStarted`, if no expansion has triggered(status.phase==""). 
   
3) Check if the expansion is in progress:
   
   1) Query actualOsdCount and actualOsdSize from the from Prometheus .
   
   2) Calculate the desiredOsdCount and desiredOsdSize from the Storagecluster.
   
   3) If the (actualOsdSize < desiredOsdSize || actualOsdCount < desiredOsdCount), expansion is in progress. 
   
4) If the expansion is in progress, check the progress and then requeue each 1 minute until the expansion is completed successfully, change the phase to `failed` if scaling not `Succeeded` with in timeoutSeconds(default:1800) interval.
   
5) If no-expansion is in progress,
   
   1) Check the (status.phase == "InProgress"), if yes scaling is successful, update the status of the `StorageAutoScaling` CR with `lastExpansionCompletionTime` and `phase` and also osd count and size.
   
   2) Proceed with further steps.

6) Alert the user if the phase changes to `Succeeded` or `Failed`, alerting will be implemented with ocs-metrics-exporter.

7) If the LSO storageclass is detected in the storageClassDeviceSet, raise a warning and do not recocnile further.
   
8) If the highest osd percentage reported in the sync map is more than osdScalingThresholdPercent(70%) means reaching osd nearfull, scaling is needed.

9)  If scaling is needed calculate the `expectedOsdSize` and `expectedOsdCount`.
       
    1)  If the Osd size is less than maxOsdSize(default:8Tib), do vertical scaling by doubling the each osd sizes for that device class.
            
    2)  If the Osd size is equal to maxOsdSize(default:8Tib), do a horizontal scaling, by adding 1 osd of maxOsdSize(default:8Tib) on each `storageDeviceSet`.
   
10) Calculate the `expectedStorageCapacity` based on expected size and count.
   
11) Check if the `storageCapacityLimit` > `expectedStorageCapacity`.
        
    1) If yes, Update `phase` to `InProgress`  and `lastExpansionStartTime` as `current-time` on the `StorageAutoScaling` CR which need scaling.
        
    2) If no, don't reconcile further and the set the `storageCapacityLimitReached` as true in the status.

12) Update the status, set the `expectedOsdSize` and `expectedOsdCount` to reflect the new expected value and also set `startOsdSize` and `startOsdCount` with current storagecluster values.
   
13) Scale by patching the Storagecluster, with all the device sets update needed at the same time.
    
14) Requeue so verification can happen.

### InProgress state:

Based on the above algorithm there would be two conditions where in-progress is set, elaborating those conditions,

1) If scaling is just started:

    1) Set `phase` to `InProgress`.
   
    2) Verify is the scaling is successful.

    3) If the scaling is successful set the `phase` to `Succeeded`.

    4) Alert the user if the phase changes to `Succeeded`, alerting will be implemented with ocs-metrics-exporter.
   
    5) If the scaling is not yet completed requeue every 1 min, we have the 2nd case. 

2) If the scaling has already started and its requeue

    1) Now the requeue will happen every 1 min.

    2) At the start of reconcile will match that `startOsdSize` and `expectedOsdSize` is not equal and similar for osd count.

    3) And another validation will do is equating storagecluster spec with prometheus response.

    4) Will requeue till the scaling is in-progress.

    5) If the scaling is in-progress with more than timeoutSeconds(default:1800) interval we set the phase to `failed`.

    6) Alert the user if the phase changes to `Failed`, alerting will be implemented with ocs-metrics-exporter. 

    7) If there as a failure alert, provide a mitigation guide for the user. 

## Failure case

There might cases where system resources are exhausted and new Osds cannot be started.

