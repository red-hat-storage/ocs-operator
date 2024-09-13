# FEATURE: Regional Disaster Recovery Optimization

## Summary

Implement use of `bluestore-rdr` as the OSD backend store for customers using ODF with Regional Disaster Recovery (RDR).

## Motivation

- Ceph uses BlueStore as a special-purpose storage back end designed specifically for managing data on disk for Ceph OSD workloads. Ceph team is working on improving this backend storage that will help in significantly improving the performance, especially in disaster recovery scenarios. This new OSD backend is known as bluestore-rdr. In the future, other backends will also need to be supported, such as seastore.

## Goals

- Customers should be able to use the optimized OSD store when creating a new ODF cluster
  
## Non Goals/Deferred Goals

- Customers should be able to update their existing ODF clusters to use the optimized OSD store

## Proposal

- Enable the optimized OSD backend store for new RDR clusters.


### Greenfield
- If `ocs.openshift.io/clusterIsDisasterRecoveryTarget: true` annotation is available in the `StorageCluster` then configure OSD store type in the `cephCluster` as:

```YAML
spec:
  storage:
    store:
        type: bluestore-rdr
```
- Customers should not be able to remove the optimizations once they are applied. If the annotation is removed `storageCluster` some how, operator should ensure that OSD store optimization settings are not overridden in the `cephCluster` CR