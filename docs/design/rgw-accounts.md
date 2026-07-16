# RGW Account Support for Hub-Spoke Clusters

## Overview

RGW Accounts (Ceph Squid / v19) group users and roles under shared ownership where the resources belong to the Account, not individual users. This gives each spoke an isolated S3 tenant with its own quotas, IAM policies and bucket visibility.

OBCs cannot fill this role because they provision one bucket with one credential and have no support for creating additional buckets, managing IAM users, or attaching policies.

This feature provisions per-spoke RGW Accounts on the hub, delivers root user credentials to spokes via the existing gRPC channel and handles spoke offboarding with Account cleanup.

## Feature Gating

Account creation requires two gates. One at the cluster level and one at the spoke level. Both must be present.

### StorageCluster Gate

Two annotations on StorageCluster control RGW deployment and advanced features independently.

- **`ocs.openshift.io/enable-advanced-rgw-features: "true"`** — enables advanced RGW features (Accounts, STS and any future capabilities). On platforms where RGW is already deployed (bare metal), this enables the features on the existing RGW.
- **`ocs.openshift.io/enable-rgw: "true"`** — forces RGW deployment on platforms where it is not normally deployed (e.g., cloud). Not needed on bare metal.
- The deploying operator sets these annotations.

#### Annotation Removal Behavior

- Removing `enable-rgw` is a no-op. The RGW remains running.
- Removing `enable-advanced-rgw-features` stops creating new Accounts, but existing Accounts and credentials remain active.

### StorageConsumer Gate

Not all spokes need an RGW Account. The hub admin must be able to control which spokes receive Account credentials.

- **Annotation:** `ocs.openshift.io/enable-rgw-account: "true"` on StorageConsumer
- Set by the deploying operator or hub admin
- Controls which spokes get an Account. 
- Without this, a spoke is unaffected by the feature even if the hub-level gate is set.

#### Internal Consumer

The internal consumer (hub itself) can also receive an RGW Account. Both annotations are required: `enable-advanced-rgw-features` on StorageCluster and `enable-rgw-account` on the internal StorageConsumer. The `enable-rgw-account` annotation is not auto-propagated. The hub admin or deploying operator must explicitly set it on the internal StorageConsumer. The use case is running S3-consuming applications directly on the hub.


## Create RGW Account per Spoke

Each spoke (StorageConsumer CR) needs an isolated RGW Account whose root user credentials can be delivered to the spoke.

When RGW is running (natively or via `enable-rgw`), `enable-advanced-rgw-features` is set on StorageCluster and `enable-rgw-account` is set on StorageConsumer, OCS Operator creates a `CephObjectStoreAccount` for the spoke. Rook creates the Account's root user and generates a credentials Secret.

### Account CR

- `metadata.name`: derived from the StorageConsumer and stored in the consumer's resource mapping ConfigMap (key: `rgw-account-name`). This follows the existing pattern where all sub-resource names reconciled by a StorageConsumer are stored in its ConfigMap.
- `spec.name`: same as `metadata.name` (ConfigMap-derived). This follows the CephFS SubVolumeGroup pattern where both fields use the same value. Visible as a display name in `radosgw-admin account list`.
- `spec.accountID`: not set. RGW auto-generates a unique ID (format: `RGW` + 17 digits). This becomes the real identifier used in IAM ARNs and the `AWS_ACCOUNT_ID` credential field.
- `spec.store` points to the CephObjectStore
- OwnerRefs: StorageConsumer (controller reference) and the consumer's resource mapping ConfigMap (owner reference). This follows the existing dual-owner pattern used for RadosNamespaces and SubVolumeGroups. Only the primary consumer (the one that created the ConfigMap) creates the Account CR. The ConfigMap ownerRef ensures the Account survives if the primary consumer is deleted while other consumers still share the ConfigMap. See "Multiple Consumers Sharing a ConfigMap" for the shared Account model.

Note: RGW account names (`spec.name`) must match `^[a-zA-Z0-9 ._-]+$` (max 2048 chars). The ConfigMap-derived name follows DNS naming, which is a subset of this regex. 

#### Root User

- Root User creation is **not** skipped. Rook creates the root user and stores its credentials in a Kubernetes Secret whose name is published in `CephObjectStoreAccount.status.rootAccountSecretName`.
- The root user has default S3 access and full IAM management permissions on all resources within the account. This allows the spoke to self-manage its IAM users, policies and S3 access without hub intervention.
- Account users (non-root) start with zero permissions and require IAM policies for any S3 or IAM operation. Only the root user can use the IAM API by default. Delivering root user credentials is the intended Ceph Account model for delegating account management.
- Account owners are encouraged to use the root user for management only and create users and roles with fine-grained permissions for specific applications.
- For recovery, the hub admin can use `radosgw-admin` via the toolbox.


### Resource Name Mapping

The consumer's resource mapping ConfigMap is extended with two new keys for shared resources:

- `rgw-account-name`: name of the `CephObjectStoreAccount` CR
- `rgw-credentials-secret-name`: name of the credentials Secret advertised to the spoke

The gRPC layer uses the ConfigMap keys to look up the Account and spoke Secret name. The root user credentials are read from the Secret referenced by `CephObjectStoreAccount.status.rootAccountSecretName`.

**Note:** The Account CR name (`rgw-account-name`) must be deterministically derived from immutable StorageConsumer properties. This ensures that if the ConfigMap is deleted and recreated, the regenerated defaults match the existing CRs. Only admin-overridden values (e.g. a custom `rgw-credentials-secret-name` for multi-hub) would be lost on ConfigMap deletion.


## Advertise Credentials via gRPC

The spoke needs the Account ID, root user credentials and RGW endpoint to connect to the hub's RGW.

Extend the existing gRPC response to include a fully-formed Secret as a KubeObject payload.

### Credential Payload

The Secret name is stored in the consumer's resource mapping ConfigMap (key: `rgw-credentials-secret-name`). The default name is `spoke-account-iam-credentials`. For multi-hub scenarios where a spoke is onboarded to multiple hubs, the admin can override this value in the ConfigMap to avoid collisions. The Secret is labeled with `ocs.openshift.io/storageclient=<name>` for stable discovery regardless of the Secret name.

The hub constructs the credentials Secret containing:

| Field | Source |
|-------|--------|
| `AWS_ENDPOINT_URL` | RGW HTTPS Route endpoint |
| `AWS_ACCOUNT_ID` | `CephObjectStoreAccount.status.accountID` |
| `AWS_ACCESS_KEY_ID` | Root user's Rook-generated Secret (`AccessKey` field), referenced by `CephObjectStoreAccount.status.rootAccountSecretName` |
| `AWS_SECRET_ACCESS_KEY` | Root user's Rook-generated Secret (`SecretKey` field), referenced by `CephObjectStoreAccount.status.rootAccountSecretName` |

- Credentials are only advertised after the Account CR reports `status.phase == "Ready"` and `status.rootAccountSecretName` is populated.
- If the Account is not ready, the Secret is omitted from the response. The spoke gets it on the next reconciliation cycle.
- The `resourceVersion` of the Rook-generated credentials Secret is included in the `desiredClientConfigHash` computed in `ReportStatus`. This ensures the spoke detects credential changes and triggers a re-sync.

**Note:** ODF does not pass a CA certificate for the RGW endpoint. The endpoint is an OCP Route. It is the hub admin's responsibility to ensure the Route's TLS certificate is trusted by spoke applications.


## Apply Credentials on Spoke

- The spoke must receive the `spoke-account-iam-credentials` Secret. 
- Spoke must keep this secret in sync with the hub.
- If the hub stops including the Secret in the gRPC response (for reasons like annotation removed, Account not ready, offboarding, etc.), the Client Operator's desired-state reconciliation detects it as absent and deletes it on the next cycle. Secret (Kind) is already in the Client Operator's managed resource list.

## Spoke-Side RBAC for Credentials Secret

The credentials Secret contains Account root user credentials. This Secret lives in the `openshift-storage` namespace on the spoke. The consuming application, running in its own namespace, won't be able to access it by default.

ODF does not manage RBAC for this Secret. The consuming application is responsible for creating whatever RBAC it needs (e.g., Role/RoleBinding granting its ServiceAccount cross-namespace read access to the Secret).

The Secret is labeled with `ocs.openshift.io/storageclient=<name>`, so consumers can discover the correct Secret in multi-hub scenarios (e.g., `oc get secret -n openshift-storage -l ocs.openshift.io/storageclient=<name>`).

## S3 Access and IAM Policies

The root user has default permissions on all resources within the account, including S3 data operations and IAM management. Non-root account users start with zero permissions and require IAM policies attached via the IAM API. Only the root user can use the IAM API by default. The spoke uses the root user credentials to create IAM users with fine-grained S3 policies tailored to each application's needs.

## Spoke Offboarding

When a spoke is offboarded, credentials are revoked and all associated resources are cleaned up automatically via ownership cascade.

1. StorageClient CR is deleted on the spoke
2. Client Operator performs checks and initiates offboard (calls `OffboardConsumer()`)
3. On success, the finalizer on StorageClient is removed
4. Kubernetes garbage-collects all StorageClient dependents, including the credentials Secret on the spoke
5. On the hub, StorageConsumer controller removes its finalizer and Kubernetes garbage-collects dependents. The ConfigMap is GC'd only if no other consumer co-owns it. The Account CR is GC'd only when both its ownerRefs (StorageConsumer and ConfigMap) are gone:
   - `CephObjectStoreAccount`: Rook deletes the Account, its root user and invalidates the root user's access keys at the RGW level

After this, the spoke has no credentials, the access keys are invalid in RGW and the Account is removed.

### Force Deletion

When the `ocs.openshift.io/force-deletion` annotation is set on StorageConsumer, the controller propagates the `rook.io/force-deletion` annotation to the `CephObjectStoreAccount` CR. This follows the same pattern used for RadosNamespace and SubVolumeGroup force-deletion. Rook's Account controller will handle the annotation by purging the Account's contents (users, buckets) before deleting the Account. This is new upstream Rook work. `CephObjectStoreAccount` does not support the force-deletion annotation today.

## Multiple Consumers Sharing a ConfigMap

Multiple StorageConsumers can reference the same resource mapping ConfigMap. Today, shared sub-resources like RadosNamespace and SubVolumeGroup are created by the primary consumer (the one that created the ConfigMap) and used by all consumers sharing it.

RGW Account creation follows the same `isPrimaryConsumer` gate. The Account is a shared resource created by the primary consumer.

### Behavior

**Case 1: Primary has annotation, secondary has annotation**
- Primary creates the Account CR (with root user)
- Both consumers' gRPC responses include the same root user credentials (same Account, same root user)

**Case 2: Primary has annotation, secondary does not**
- Primary creates Account, gets root user credentials
- Secondary is unaffected. No credentials shared

### Credential Isolation

When multiple consumers share an Account, they share the same root user credentials. Credential isolation between consumers sharing an Account is managed at the IAM level — each spoke can create its own IAM users with distinct access keys using the root user's IAM management capabilities.

## Limitations and Future Work

- **Multiple CephObjectStore support:** The current design assumes a single CephObjectStore. Multiple stores are a possibility in the future (e.g. Fusion might want separate stores with different configurations such as a D4N-cached store for high-performance workloads and a default store for everything else). The MVP uses unqualified ConfigMap keys and CR names. Multi-store can be layered on without redesigning the MVP:

  1. If the ConfigMap has no store-selection key, assume the default store (`ocs-storagecluster-cephobjectstore`). This preserves backward compatibility with the MVP.
  2. If a store-selection key is present, use the specified store instead of the default.
  3. If a spoke needs Accounts on multiple stores, introduce a new field with `{store, account}` pairs that overrides the older single-store keys.
  4. ODF does not support automatic data migration when store assignments change. The hub admin would handle the transition following a KCS.

  Hub admins control which store is advertised to which spoke. A spoke may receive one store or multiple stores depending on its workload requirements.

- **Non-primary consumer triggering Account creation:** Account creation is gated on `isPrimaryConsumer`, matching the existing pattern for RadosNamespace and SubVolumeGroup. If the primary consumer does not have `enable-rgw-account` but a non-primary consumer does, the Account will not be created. Supporting this would require changing the Account's ownership model (using `SetOwnerReference` instead of `SetControllerReference` so any consumer can create it without controller conflicts).

- **Force-deletion with shared Accounts:** For shared Accounts, multiple ownerRefs keep the Account alive until the last owner is deleted. Force-deletion of one consumer won't GC the Account but leaves a stale force-deletion annotation that could cause unintended purge when the last consumer is removed.

- **Root user credential rotation:** Rook does not support declarative key rotation for the Account root user. Rotation currently requires manual intervention via `radosgw-admin key create` from the toolbox. If keys are rotated this way, the propagation pipeline handles it. Rook updates the root user credentials Secret, the `resourceVersion` changes, `desiredClientConfigHash` changes and the spoke re-syncs.

- **Internal consumer migration to Accounts:** The internal consumer (hub) can receive an RGW Account under this design if both annotations are set. This gives the hub a new Account-based access path to RGW. Existing OBC-based workloads on the hub continue as-is. Migration of those workloads to use Account credentials is not planned for this feature. The hub admin could do it manually since Account credentials are delivered to the hub the same way as to any spoke.
