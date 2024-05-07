# RBD Volume Migration Tool

The RBD Volume Migration Tool facilitates the migration of Ceph-CSI RBD volumes
between Ceph pools.

## Overview

Presently, the tool is designed to be run as a Job within the same cluster(s)
as an OCS provider/consumer installation. On the consumer side, it expects a
Ceph-CSI StorageClass with a configured `clusterId`. On the provider side, it
expects a StorageRequest to help track the state of the migration operation
and auto-detect the likely source and target image locations.

It also makes use of the RBD image live-migration feature to facilitate
fault-tolerant data transfer between pools. The actual "live" part is currently
not supported due to limitations in the krbd kernel module used by Ceph-CSI.

## Running the Tool

The tool is designed to be run as a Job within the same cluster(s) as any OCS
provider/consumer installation. To facilitate this, the tool has a `dump-yaml`
command which will generate the Job YAML that can get be piped into an `oc`
command, as below:

`volume-migration -m [mode] dump-yaml | oc apply -f -`

### Usage

```help
Usage:
  volume-migration dump-yaml [flags]

Flags:
  -h, --help   help for dump-yaml

Global Flags:
      --cluster-id string        StorageClass ClusterID of the volumes to be migrated
      --context string           kubecontext to use
      --dry-run                  no commitments
  -i, --image string             container image for volume migration Job (default "quay.io/ocs-dev/ocs-volume-migration:latest")
      --kubeconfig string        kubeconfig path
  -m, --mode string              'converged', 'provider', or 'consumer' (default "converged")
  -f, --state-file string        path to file where program will store state (default "state.json")
      --storage-request string   name of StorageRequest to migrate
```

### Pre-requisites

The tool expects all Pods mounting PVs from the StorageClass being migrated to
be terminated. Currently it will loop through all RBD images discovered in the
source location and migrate them one at a time, so the admin must wait for all
images to finish migrating before restarting any application Pods.

NOTE: In addition, for a migration from 4.15 to 4.16, specifically, the tool
can look for a reference to a 4.15 CephBlockPool *in the StorageRequest Status*
to use as the migration source. If this is not present, the source location
must be provided manually by the admin.
