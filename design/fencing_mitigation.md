# Problem Statement

We need to ensure that only one node can be fenced at a time and that Ceph is fully recovered (has healthy state) before any fencing is initiated. The available pattern for limiting fencing is the MachineDisruptionBudget which allows us to specify maxUnavailable. However, this won’t be sufficient to ensure that Ceph has recovered before fencing is initiated.

Therefore, we will control how many nodes match the MDB by dynamically adding and removing labels as well as dynamically updating the MDB. By manipulating the MDB into a state where desiredHealthy > currentHealthy, we can disable fencing on the nodes the MDB points to.

# Design:

## machine-controller
Each `Machine` part of a `StorageCluster` will be labeled with the key `fencegroup.openshift.io/<cluster-name>.

The node label controller will maintain a label on the `Machine` as long as there are osds scheduled there that are not down. Therefore fencing will only be allowed on nodes with no OSDs up while the cluster is not healthy.


The contoller will know which osds are down by querying the OSD `Deployment` objects.

## machinedisruptionbudget-controller
Whenever a `StorageCluster` is created, a corresponding `MachineDisruptionBudget` will be created that is dynamically controlled by this controller. It will point at `Machine`s labeled `fencegroup.openshift.io/<cluster-name>`. It has no effect on `Machine`s without the label.

The controller will turn off fencing whenever ceph health is not HEALTH_OK by changing maxUnavailable to 0, and it will turn it on by changing maxUnavailable to 1.

## Assumptions:
- We assume that the controllers will be able to reconcile before fencing proceeds. We do know that fencing will happen only after a configurable timeout. The default timeout is 5 minutes.

## Example Flow:
 - Node has NotReady condition.
 - Ceph becomes unhealthy
 - machinedisruptionbudget-controller sets maxUnavailable to 0 on the MachineDisruptionBudget.
 - MachineHealthCheck sees NotReady and attempts to fence, but can't due to MDB
 - machine-controller notices all OSDs on the affected node are down and removes the node from the MDB.
 - MDB no longer covers the affected node, and MachineHealthCheck fences it.

## What is fencing?

Once [MachineHealthCheck controller](https://github.com/openshift/machine-api-operator#machine-healthcheck-controller) detects that a node is `NotReady` (or some other configured condition), it will remove the associated `Machine` which will cause the node to be deleted. The `MachineSet` controller will then replace the `Machine` via the machine-api. The exception is on baremetal platforms where fencing will reboot the underlying `BareMetalHost` object instead of deleting the `Machine`.

## Why can't we use `PodDisruptionBudget`?

Fencing does not use the eviction api.

## Will we need to do large storage rebalancing after fencing?

Hopefully not. On cloud platforms, the OSDs can be rescheduled on new nodes along with their backing PVs, and on baremetal where the local PVs are tied to a node, fencing will simply reboot the node instead of destroying it.
