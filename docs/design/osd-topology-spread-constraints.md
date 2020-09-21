# FEATURE: OSD Topology Spread Constraints

## Summary

Implement the use of [Topology Spread Constraints] to distribute the Ceph OSD
Pods across the configured failure domains.

## Motivation

Currently, the primary mechanism for spreading the OSD Pods is to use Pod
AntiAffinities. In addition, the OSD Pods are grouped into three
StorageClassDeviceSets (SCDS) in the CephCluster CR, with each SCDS locked to a
specific failure domain. This presents two problems:

1. AntiAffinity breaks down when there are more Pods than available Nodes. As
   soon as this happens, the Kubernetes scheduler acts as if the AntiAffinity
   doesn't exist, which means that an even distribution of Pods can not be
   guaranteed.
2. OSD Pods are locked to only three failure domains, even when more than three
   exist.

### Goals

Provide an even-as-possible distribution of OSD Pods across all available
failure domains based on Pod count as opposed to the default of resource
availability.

### Non-Goals

Always maintain an even distribution of OSD Pods. OSDs can scale at a rate that
does not match the number of Nodes in a given failure domain, as such there
will sometimes be uneven distribution of OSD Pods across Nodes in a given
failure domain.

## Proposal

Implement the use of Topology Spread Constraints for Ceph OSD Pods. Include the
ability to specify them in the `StorageCluster.StorageDeviceSet` spec so they
can be propogated to the resulting `CephCluster`'s SCDSes, providing sane
defaults if they are not specified.

### Risks and Mitigations

There is an issue in upgrade on the Rook-Ceph side that needs to be addressed.
The issue is that we want to change the generation of SCDSes from multiple sets
per StorageDeviceSet to one. This would cause a change in the name generation
for OSD Pods, which would be a problem in upgrade scenarios going from three
SCDSes to one.

The initial reason for having multiple StorageClassDeviceSets was to force an
even distribution of OSD Pods across failure domains (initially, AWS
Availability Zones (AZs)), since we could not achieve this with Node and Pod
affinities. Topology Spread Constraints removes the need for this hack.

We will mitigate this by making an initial patch to partially implement
Topology Spread Constraints in ocs-operator while still maintaining backwards
compatibility with Rook-Ceph until the upgrade issue can be addressed.


## Design Details

The OSD Placement will be modified as follows:

1. OSD Prepare Jobs will be spread across failure domains (zone/rack).
2. OSD daemon Pods will only be spread across hosts. The PVs created by the
   corresponding prepare Jobs will contain further restrictions on where the PV
   can be mounted, thus influencing the scheduling of the OSD daemon Pod to the
   correct failure domain.
3. All Pods would still maintain the default NodeAffinity to only run on nodes
   selected for OCS (if configured).

The changes will come in two phases:

**Phase 1**

Change the default Placement of StorageDeviceSets to use Topology Spread
Constraints, but maintain the split to three StorageClassDeviceSets in the
generated CephCluster. This preserves the naming scheme for the OSD Pods that
Rook-Ceph currently implements.

Since we will no longer need to explicitly distinguish between exact failure
domains, the constraints themselves would be identical across all SCDSes. This
means that all Pods will be schedulable across all failure domains, regardless
of which SCDS it belongs to.

For upgrade scenarios, existing OSD Pods would still remain with the old
Placement configurations. However, because all OSD Pods have appropriate app
labels to identify them, they would still influence any new OSD Pods created
with Topology Spread Constraints, allowing the new OSD Pods to be scheduled
in a more even pattern across failure domains.

**Phase 2**

The currently proposed solution for the upgrade scenario is to enhance
Rook-Ceph to detect existing OSD Pods and determine if they are valid for a
given SCDS. Once this is done, we will be able to submit a final patch to
change the CephCluster generation from three SCDSes to one.

### Test Plan

These changes are under the hood changes, and should require no additional
e2e testing in our CI. We would need to make sure we're looking for an
appropriate number of SDCSes in the generated CephCluster.

### Monitoring Requirements

None

### Dependencies

None

## Alternatives

[Topology Spread Constraints]: https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints/
