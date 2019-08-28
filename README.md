# The OCS Meta Operator

This is the primary operator for Red Hat OpenShift Container Storage (OCS). It
is a "meta" operator, meaning it serves to facilitate the other operators in
OCS by performing administrative tasks outside their scope as well as
watching and configuring their CustomResources (CRs).

## Build

### OCS Operator
The operator is based on the [Operator
SDK](https://github.com/operator-framework/operator-sdk). In order to build the
operator, you first need to install the SDK. [Instructions are
here.](https://github.com/operator-framework/operator-sdk#quick-start)

Once the SDK is installed, the operator can be built via:

```console
$ dep ensure --vendor-only

$ make ocs-operator
```

### Converged CSV

The converged CSV is created by sourcing manifests from each component-level
operator to create a single unified CSV capable of deploying the component-level
operators as well as the ocs-operator.

Building the unifed CSV is broken into two steps which are supported by the
`make source-manifests` and `make gen-csv` make targets.

**Step 1:** Source in the component-level manifests from the component-level operator
container images.

Set environment variables referencing the ROOK and NOOBAA container images
to source the CSV/CRD data from. Then execute `make source-manifests`

```
$ export ROOK_IMAGE=<add rook image url here>
$ export NOOBAA_IMAGE=<add noobaa image url here>
$ make source-manifests
```

The above example will source manifests in from the supplied container images
and store those manifests in `build/_outdir/csv-templates/`

**Step 2:** Generate the unified CSV by merging together all the manifests
sourced in from step 1.

Set environment variables related to CSV versioning.

Also, set environment variables representing the container images that should be
used in the deployments.


**NOTE: Floating tags like 'master' and 'latest' should never be used in an official release.**

```
$ export CSV_VERSION=0.0.2
$ export REPLACES_CSV_VERSION=0.0.1
$ export ROOK_IMAGE=<add rook image url here>
$ export NOOBAA_IMAGE=<add noobaa image url here>
$ export OCS_IMAGE=<add ocs operator image url here>
$ export ROOK_CSI_CEPH_IMAGE=<add image here>
$ export ROOK_CSI_REGISTRAR_IMAGE=<add image here>
$ export ROOK_CSI_PROVISIONER_IMAGE=<add image here>
$ export ROOK_CSI_SNAPSHOTTER_IMAGE=<add image here>
$ export ROOK_CSI_ATTACHER_IMAGE=<add image here>

$ make gen-csv
```

This example results in both a unified CSV along with all the corresponding CRDs being placed in `deploy/olm-catalog/ocs-operator/0.0.2/` for release.

Run `make ocs-registry` to generate the registry bundle container image.

## Install

The OCS operator can be installed into an OpenShift cluster using the OLM.

For quick install using pre-built container images, deploy the [deploy-olm.yaml](deploy/deploy-with-olm.yaml) manifest.

```console
$ oc create -f ./deploy/deploy-with-olm.yaml
```

This creates:

* a custom CatalogSource
* a new `openshift-storage` Namespace
* an OperatorGroup
* a Subcription to the OCS catalog in the `openshift-storage`
namespace

You can check the status of the CSV using the following command:

```console
$ oc get csv -n openshift-storage
NAME                  DISPLAY                                VERSION   REPLACES   PHASE
ocs-operator.v0.0.1   Openshift Container Storage Operator   0.0.1                Succeeded
```
This can take a few minutes. Once PHASE says `Succeeded` you can create
a StorageCluster by following command:

```console
$ oc create -f ./deploy/crds/ocs_v1alpha1_storagecluster_cr.yaml
```

### Installation of development builds

To install own development builds of OCS, first build and push the ocs-operator image to your own image repository.

```console
$ export REGISTRY_NAMESPACE=<quay-username>
$ export IMAGE_TAG=<some-tag>
$ make ocs-operator
$ podman push quay.io/$REGISTRY_NAMESPACE/ocs-operator:$IMAGE_TAG
```

Once the ocs-operator image is pushed, edit the CSV to point to the new image.

```
$ OCS_OPERATOR_IMAGE="quay.io/$REGISTRY_NAMESPACE/ocs-operator:$IMAGE_TAG"
$ sed -i "s|quay.io/ocs-dev/ocs-operator:latest|$OCS_OPERATOR_IMAGE" ./deploy/olm-catalog/ocs-operator/0.0.1/ocs-operator.v0.0.1.clusterserviceversion.yaml
```

Then build and upload the catalog registry image.

```console
$ export REGISTRY_NAMESPACE=<quay-username>
$ export IMAGE_TAG=<some-tag>
$ make ocs-registry
$ podman push quay.io/$REGISTRY_NAMESPACE/ocs-registry:$IMAGE_TAG
```

Next create the namespace for OCS and create an OperatorGroup for OCS
```console
$ oc create ns openshift-storage

$ cat <<EOF | oc create -f -
apiVersion: operators.coreos.com/v1alpha2
kind: OperatorGroup
metadata:
  name: openshift-storage-operatorgroup
  namespace: openshift-storage
EOF
```

Next add a new CatalogSource using the newly built and pushed registry image.
```console
$ cat <<EOF | oc create -f -
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: ocs-catalogsource
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: quay.io/$REGISTRY_NAMESPACE/ocs-registry:$IMAGE_TAG
  displayName: OpenShift Container Storage
  publisher: Red Hat
EOF
```

Finally subscribe to the OCS catalog.
```console
$ cat <<EOF | oc create -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: ocs-subscription
  namespace: openshift-storage
spec:
  channel: alpha
  name: ocs-operator
  source: ocs-catalogsource
  sourceNamespace: openshift-marketplace
EOF
```

## Initial Data

When the operator starts, it will create a single OCSInitialization resource. That
will cause various initial data to be created, including default
StorageClasses.

The OCSInitialization resource is a singleton. If the operator sees one that it
did not create, it will write an error message to its status explaining that it
is being ignored.

### Modifying Initial Data

You may modify or delete any of the operator's initial data. To reset and
restore that data to its initial state, delete the OCSInitialization resource. It
will be recreated, and all associated resources will be either recreated or
restored to their original state.
