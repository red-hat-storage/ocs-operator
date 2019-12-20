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

### OLM catalog

Update `hack/generate-latest-csv.sh` with the image versions to be used, and run

```console
$ make gen-latest-csv
```

to generate/update the OLM catalog for OCS-operator.

A catalog registry image can be then generated using,

```console
$ make ocs-registry
```

More details on the catalog generation are avilable in [docs/catalog-generation.md](docs/catalog-generation.md).

## Prerequisites

OCS Operator will install its components only on nodes labelled for OCS with the key `cluster.ocs.openshift.io/openshift-storage=''`.
To label the nodes from CLI,

```console
$ oc label nodes <NodeName> cluster.ocs.openshift.io/openshift-storage=''
```

OCS requires at least 3 nodes labelled this way.

When creating StorageCluster from the UI, the create wizard takes care of labelling the selected nodes.

### Dedicated nodes

In case dedicated storage nodes are available, these can also be tainted to allow only OCS components to be scheduled on them.
Nodes need to be tainted with `node.ocs.openshift.io/storage=true:NoSchedule` which can done from the CLI as follows,

```console
$ oc adm taint nodes <NodeNames> node.ocs.openshift.io/storage=true:NoSchedule
```

> Note: The dedicated/tainted nodes will only run OCS components. The nodes will not run any apps. Therefore, if you taint, you need to have additional worker nodes that are untainted. If you don't, you will be unable to run any other apps in you Openshift cluster.

> Note: Currently not all OCS components have the right tolerations set. So if you taint nodes and do not have additional untainted nodes, OCS will fail to deploy.

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
a StorageCluster.

A StorageCluster resource can now be created from the console, using the StorageCluster creation wizard.
From the CLI, a StorageCluster resource can be created using the example CR as follows,

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

## Functional Tests

Our functional test suite uses the [ginkgo](https://onsi.github.io/ginkgo/) testing framework.

**Prerequisites for running Functional Tests**
- ocs must already be installed
- KUBECONFIG env var must be set

**Running functional test**

```make functest```

Below is some sample output of what to expect.

```
Building functional tests
hack/build-functest.sh
GINKO binary found at /home/dvossel/go/bin/ginkgo
Compiling functests...
    compiled functests.test
Running functional test suite
hack/functest.sh
Running Functional Test Suite
Running Suite: Tests Suite
==========================
Random Seed: 1568299067
Will run 1 of 1 specs

•
Ran 1 of 1 Specs in 7.961 seconds
SUCCESS! -- 1 Passed | 0 Failed | 0 Pending | 0 Skipped
PASS
```

**Functional test phases**

There are 3 phases to the functional tests to be aware of.

1. **BeforeSuite**: At this step, the StorageCluster object is created and the test
blocks waiting for the StorageCluster to come online.

2. **Test Execution**: Every written test can assume at this point that a
StorageCluster is online and PVC actions should succeed.

3. **AfterSuite**: This is where test artifact cleanup occurs. Right now all tests
should execute in the `ocs-test` namespace in order for artifacts to be cleaned
up properly.

NOTE: The StorageCluster created in the BeforeSuite phase is not cleaned up.
If you run the functional testsuite multiple times, BeforeSuite will simply
fast succeed by detecting the StorageCluster already exists.

### Developing Functional Tests

All the functional test code lives in the ```functests/``` directory. For an
example of how a functional test is structured, look at the ```functests/pvc_creation_test.go```
file.

The tests themselves should invoke simple to understand steps. Put any complex
logic into separate helper files in the functests/ directory so test flows are
easy to follow.

**Running a single test**
When developing a test, it's common to just want to run a single functional test
rather than the whole suite. This can be done using ginkgo's "focus" feature.

All you have to do is put a ```F``` in front of the tests declaration to force
only that test to run. So, if you have an iteration like ```It("some test")```
defined, you just need to set that to ```FIt("some test")``` to force the test
suite to only execute that single test.

Make sure to remove the focus from your test before creating the pull request.
Otherwise the test suite will fail in CI.

### Debugging Functional Test Failures

If an e2e test fails, you have access to two sets of data to help debug why the
error occurred.

**Functional test stdout log**

This will tell you what test failed and it also outputs some debug information
pertaining to the test cluster's state after the test suite exits. In prow you
can find this log by clicking on the `details` link to the right of the
`ci/prow/ocs-operator-e2e-aws` test entry on your PR. From there you can click
the `Raw build-log.txt` link to view the full log.

**PROW artifacts**

In addition to the raw test stdout, each e2e test result has a set of artifacts
associated with it that you can view using prow. These artifacts let you
retroactively view information about the test cluster even after the e2e job
has completed.

To browse through the e2e test cluster artifacts, click on the `details` link
to the right of the `ci/prow/ocs-operator-e2e-aws` test entry on your PR. From
there look at the top right hand corner for the `artifacts` link. That will
bring you to a directory tree. Follow the `artifacts/` directory to the
`ocs-operator-e2e-aws/` directory. There you can find logs and information
pertaining to ever object in the cluster.

## Downstream ocs-ci tests

In addition to the `functest/*` in the ocs-operator source tree we also have
the ability to run the tests developed in the [red-hat-storage/ocs-ci](https://github.com/red-hat-storage/ocs-ci) repo.

NOTE: Running this test suite requires python3.

To execute the ocs-ci test suite against an already installed ocs-operator, run
`make red-hat-storage-ocs-ci`. This will download the ocs-ci repo and execute
a subset of tests against the ocs-operator deployment.

In order to update either the ocs-ci version or subset of tests executed from
ocs-ci, you'll need to modify the environment variables in hack/common.sh with
the `REDHAT_OCS_CI` prefix.

## Ginkgo tests vs ocs-ci tests

We currently have two functional test suites. The ginkgo `functests` test suite
lives in this repo, and there is also an external one called `ocs-ci`.

The ginkgo `functests` test suite in this repo is for developers. As new
functionality is introduced into the ocs-operator, this repo allows developers
to prove their functionality works by including tests within their PR. This is
the test suite where we exercise ocs-operator deployment/update/uninstall as
well as some basic workload functionality like creating PVCs.

The external `ocs-ci` test suite is maintained by Red Hat QE. We execute this
test suite against our PRs as another layer of verification in order to catch
rook/ceph regressions as early as possible.
