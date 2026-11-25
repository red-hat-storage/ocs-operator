# FEATURE: Configurable Ceph daemon priority classes

## Summary

Allow administrators to configure Ceph daemon PriorityClasses through
`StorageCluster.spec.managedResources.cephCluster.priorityClassNames`.
Unspecified values retain the existing OCS defaults.

Rook already supports component-specific priority classes in
`CephCluster.spec.priorityClassNames`. OCS currently generates fixed values and
replaces the child resource's spec during reconciliation. Exposing this
configuration through StorageCluster would preserve a single source of truth
without requiring administrators to disable CephCluster reconciliation.

This document proposes the API and reconciliation behavior for review.
Implementation should follow after the design is accepted.

## Motivation

Administrators may need distinct scheduling or priority-based graceful node
shutdown policies for MON and OSD daemons. Assigning both components the same
priority prevents kubelet shutdown phases from distinguishing between them.

Editing the generated CephCluster is not durable under OCS reconciliation.
Disabling reconciliation to retain a priority override also prevents OCS from
propagating unrelated configuration and Ceph image updates.

### Goals

- Allow explicit per-component overrides through the StorageCluster API.
- Preserve existing behavior when the optional configuration is absent.
- Keep OCS reconciliation and upgrade management enabled.

### Non-Goals

- Change default priority classes or automatically create PriorityClasses.
- Configure MDS, RGW, CSI or other workloads managed through separate resources.
- Configure kubelet shutdown phases, guarantee shutdown ordering or eliminate
  storage failover delays during abrupt node loss.

## Proposal

Add an optional priority-class map to the existing managed CephCluster
configuration. OCS would use it when generating the desired CephCluster spec,
and Rook would continue to manage the corresponding daemon workloads.

Administrators would create the referenced PriorityClasses separately. OCS
would not alter their priority values or preemption policies.

### Risks and Mitigations

Changing priorities can affect preemption and may cause Rook to update daemon
workloads. Administrators must account for available capacity and plan changes
through their normal storage maintenance process. No additional disruption
mechanism or relaxation of Rook's health checks is proposed.

Priority-based shutdown also depends on kubelet configuration and the
priorities of volume consumers, CSI and network agents. Lowering a storage
daemon's priority without considering these dependencies can introduce another
shutdown race. This API would provide configuration, not an automatic shutdown
policy.

Invalid or missing PriorityClasses must not silently fall back to a different
class. Reject invalid map entries through API validation; let normal
Kubernetes admission and existing Rook status reporting surface references to
classes that do not exist.

## Design Details

Extend `ManageCephCluster` with an optional `priorityClassNames` map:

```yaml
spec:
  managedResources:
    cephCluster:
      priorityClassNames:
        mon: storage-mon-critical
        osd: storage-osd-critical
```

The map would use Rook's CephCluster component keys: `all`, `mgr`, `mon`, `osd`,
`crashcollector`, `exporter` and `cleanup`. Component-specific entries take
precedence over `all`. Unknown keys and empty class names should be rejected;
omitting or removing an entry is the way to restore its default.

Generate the desired priority map in this order:

1. Start with the existing OCS defaults for MGR, MON, OSD, exporter and
   crashcollector, all currently `system-node-critical`.
2. If the administrator supplies `all`, apply that class to the default
   component entries and retain the `all` entry for Rook's fallback behavior.
3. Apply explicit component entries over the resulting map.

Applying `all` before component overrides avoids depending on Go map iteration
order. It also makes an administrator's `all` value effective despite OCS's
explicit default component entries.

An absent or empty map produces exactly the current generated priority map.
Removing an override restores the current OCS default, or inherits an
explicitly configured `all` value. Construct a fresh map on each reconciliation
without modifying the StorageCluster's map.

StorageCluster remains the source of truth. Do not copy priority values from
the live child CephCluster into the desired configuration. All other
CephCluster generation and reconciliation behavior remains unchanged.

The subsequent implementation would update the API types, generated deepcopy
code and CRD manifests using the existing generation workflow, together with
CephCluster generation and focused tests.

### Test Plan

- Verify unchanged defaults for absent and empty maps.
- Verify partial MON/OSD overrides, `all`, and component precedence.
- Reconcile an existing CephCluster after adding, changing and removing
  overrides, confirming that unrelated spec fields remain managed.
- Verify map deepcopy isolation and CRD validation of supported keys and
  non-empty, syntactically valid PriorityClass names.

No cluster mutation is required to review this design. Any later end-to-end
validation of shutdown behavior requires a separately approved maintenance
test and must distinguish graceful shutdown from abrupt power loss.

### Monitoring Requirements

No new metrics are proposed. Generated CephCluster priority values and daemon
Pod specifications expose the effective configuration; existing operator
status and Kubernetes events should surface failures.

### Dependencies

Use the existing Rook priority-class API and Kubernetes PriorityClass support.
No new controller, admission service or external dependency is proposed.

## Alternatives

**Preserve direct edits to the child CephCluster:** this makes configuration
ownership ambiguous and cannot reliably distinguish administrator overrides
from defaults previously generated by OCS, especially across upgrades.

**Set `reconcileStrategy: init` or `ignore`:** this bypasses management of the
whole CephCluster resource, not just its priority fields. It is too broad for a
per-component configuration feature.

**Patch generated Deployments or add a mutation controller:** these approaches
introduce competing ownership with Rook instead of using its existing API.

## References

- [Rook priority class names](https://rook.io/docs/rook/latest-release/CRDs/Cluster/ceph-cluster-crd/#priority-class-names)
- [Contribution flow](../../CONTRIBUTING.md#contribution-flow)
