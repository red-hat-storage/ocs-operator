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
    kind: StorageAutoScaling
    metadata:
        name: <storageclusterName>-<deviceClassName>
        namespace: openshift-storage
    spec:
        storageCluster: 
            name: <storageclusterName>
        deviceClassName: ssd
        deviceClassStorageCapacityLimit: <capacity>
        osdScalingThresholdPercent: 70%  # should be less than osd nearfull percentage 
        maxOsdSize: 8Ti
        timeoutSeconds: 1800   
    status:
        phase: (NotStarted | InProgress | Succeeded)
        lastExpansionStartTime: (always set after a new expansion is started)
        lastExpansionCompletionTime: (only set on success)
        startOsdSize: 2TiB
        expectedOsdSize: 4TiB
        startOsdCount: 3​
        expectedOsdCount: 3
    ```

- Watch prometheus periodically every 10 mins and depending on the Osd fill percentage, reconcile the StorageAutoScaling CR.
  
- If the Osd size is less than maxOsdSize(default:8Tib), do vertical scaling by doubling the each osd sizes for that device class.

- If the Osd size is equal to maxOsdSize(default:8Tib), do a horizontal scaling, by adding 1 osd of 8Tib on each storage node, by simply increasing the (count+1) in the device set associated with a device class.   

## Low-Level Design 

1) Create a controller that [requeueAfter](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/reconcile#Result) every 10 mins.
    a) If an expansion is in progress(expectedOsdSize!=startOsdSize || expectedOsdCount!=startOsdCount), check the progress and then requeue each 1 minute until the expansion is completed successfully(jump to step 7).

2) List all the device class Osds used percentage using Prometheus metrics.
    Query: (ceph_osd_metadata * on (ceph_daemon, namespace, managedBy) group_right(device_class,hostname) (ceph_osd_stat_bytes_used / ceph_osd_stat_bytes))
       `OsdPercentage`:
        ```
        osd1-ssd               69%
        osd2-ssd               72%
        ```

3) If the highest osd percentage reported is more than osdScalingThresholdPercent(70%) means reaching osd nearfull, scaling is needed.

4) If scaling is needed calculate the `expectedOsdSize` and `expectedOsdCount`.
    1)  Calculate the total number osds and osd size from the prometheus response.
    2)  Calculate the total number osds and osd size from the storageCluster.
    3)  If the above values are equal(needed to avoid double re-sizing), create or update the `OsdPercentage map`
        1)  If the Osd size is less than maxOsdSize(default:8Tib), do vertical scaling by doubling the each osd sizes for that device class.
        2)  If the Osd size is equal to maxOsdSize(default:8Tib), do a horizontal scaling, by adding 1 osd of maxOsdSize(default:8Tib) on each `storageDeviceSet`.
      
            `OsdPercentage` map:

            ```
                expectedOsdSize: 4
                expectedOsdCount: 6
            ```

5) StorageAutoScaling status update:
   1) Depending on the `OsdPercentage map` set the `expectedOsdSize` and `expectedOsdCount` to reflect the new expected value.
   2) Update `phase` to `InProgress` on the `StorageAutoScaling` CR which need scaling.
   3) Update the `lastExpansionStartTime` as `current-time`.

6) Scale with the following algorithm:
   1) Check if the totalCapacity allows to scale it or not.
   2) Patch the Storagecluster, with all the device sets update needed at the same time.

7) Verify and Alert:
   1) Verify the Storagecluster whether the new osds are added or scaled in size, for all the device sets. 
   2) If the scaling is successful will raise a `success` alert and update the status of the `StorageAutoScaling` CR with `lastExpansionCompletionTime` and `phase` and also osd count and size.
   3) If it is fails will do a requeue every 1 min and, will raise a `failure` alert if scaling not `succeed` with in timeoutSeconds(default:1800) interval.   

### InProgress state:

Based on the above algorithm there would be two conditions where in-progress is set, elaborating those conditions,

1) If scaling is just started:
    a) Set `phase` to `InProgress`.
    b) Verify is the scaling is successful. 
    c) If the scaling is successful set the `phase` to `completed`.
    b) If the scaling is un-successful requeue every 1 min, we have the 2nd case. 

2) If the scaling has already started and its requeue
    a) Now the requeue will happen every 1 min.
    b) At the start of reconcile will match that `startOsdSize` and `expectedOsdSize` is not equal and similar for osd count.
    c) And another validation will do is equating storagecluster spec with prometheus response.
    d) Will requeue till the scaling is in-progress.
    e) If the scaling is in-progress with more than timeoutSeconds(default:1800) interval we will raise a user `failure` alert.
    f) If there as a failure alert, provide a mitigation guide for the user. 

## Failure case

There might cases where system resources are exhausted and new Osds cannot be started.

