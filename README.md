# Table of Contents

- [OpenShift Container Storage Operator](#openshift-container-storage-operator)
- [Deploying pre-built images](#deploying-pre-built-images)
  - [Prerequisites](#prerequisites)
  - [Dedicated nodes](#dedicated-nodes)
  - [Installation](#installation)
- [Development](#development)
  - [Tools](#tools)
  - [Build](#build)
    1. [OCS Operator](#ocs-operator)
    2. [Unified CSV](#unified-csv)
    3. [OCS Operator Bundle](#ocs-operator-bundle)
    4. [OCS Operator Index](#ocs-operator-index)
  - [Deploying development builds](#deploying-development-builds)
- [Initial Configuration](#initial-configuration)
  - [Modifying Initial Configuration](#modifying-initial-configuration)
- [Functional Tests](#functional-tests)
  - [Prerequisites for running Functional Tests](#prerequisites-for-running-functional-tests)
  - [Running functional test](#running-functional-test)
  - [Functional test phases](#functional-test-phases)
  - [Developing Functional Tests](#developing-functional-tests)
  - [Running a single test](#running-a-single-test)
  - [Debugging Functional Test Failures](#debugging-functional-test-failures)
    - [Functional test stdout log](#functional-test-stdout-log)
    - [PROW artifacts](#prow-artifacts)
- [ocs-ci tests](#ocs-ci-tests)
- [Ginkgo tests vs ocs-ci tests](#ginkgo-tests-vs-ocs-ci-tests)

## OpenShift Container Storage Operator

This is the primary operator for Red Hat OpenShift Container Storage (OCS). It
is a "meta" operator, meaning it serves to facilitate the other operators in
OCS by performing administrative tasks outside their scope as well as
watching and configuring their CustomResources (CRs).

## Deploying pre-built images

### Prerequisites

OCS Operator will install its components only on nodes labelled for OCS with `cluster.ocs.openshift.io/openshift-storage=''`.

To label the worker nodes from CLI,

```console
$ oc label nodes <NodeName> cluster.ocs.openshift.io/openshift-storage=''
```

OCS requires at least 3 nodes labelled this way.

> Note: When deploying via Console, the creation wizard takes care of labelling the selected nodes.
>
> OpenShift cluster's "master" nodes will need to add toleration for all taints to be used, do this, or choose "worker" nodes to install on.

### Dedicated nodes

In case dedicated storage nodes are available, these can also be tainted to allow only OCS components to be scheduled on them.
Nodes need to be tainted with `node.ocs.openshift.io/storage=true:NoSchedule` which can be done from the CLI as follows,

```console
$ oc adm taint nodes <NodeNames> node.ocs.openshift.io/storage=true:NoSchedule
```

> Note: The dedicated/tainted nodes will only run OCS components. The nodes will not run any apps. Therefore, if you taint, you need to have additional worker nodes that are untainted. If you don't, you will be unable to run any other apps in you OpenShift cluster.

### Installation

The OCS operator can be installed into an OpenShift cluster using Operator Lifecycle Manager (OLM).

For quick install using pre-built container images, deploy the [deploy-olm.yaml](deploy/deploy-with-olm.yaml) manifest.

```console
$ oc create -f ./deploy/deploy-with-olm.yaml
```

This creates:

- a custom CatalogSource
- a new `openshift-storage` Namespace
- an OperatorGroup
- a Subscription to the OCS catalog in the `openshift-storage` namespace

You can check the status of the CSV using the following command:

```console
$ oc get csv -n openshift-storage
NAME                  DISPLAY                                VERSION   REPLACES   PHASE
ocs-operator.v0.0.1   OpenShift Container Storage Operator   0.0.1                Succeeded
```

This can take a few minutes. Once PHASE says `Succeeded` you can create a StorageCluster.

StorageCluster can be created from the console, using the StorageCluster creation wizard.
From the CLI, a StorageCluster resource can be created using the example CR as follows,

```console
$ oc create -f ./config/samples/ocs_v1_storagecluster.yaml
```

## Development

### Tools

- [Operator SDK](https://github.com/operator-framework/operator-sdk)

- [Kustomize](https://github.com/kubernetes-sigs/kustomize)

- [controller-gen](https://github.com/kubernetes-sigs/controller-tools)

### Build

#### OCS Operator

The operator image can be built via:

```console
$ make ocs-operator
```

#### Unified CSV

Update `hack/common.sh` with the image versions to be used, and run

```console
$ make gen-latest-csv
```

to generate unified CSV.

#### OCS Operator Bundle

To create an operator bundle image with the bundle generated above, run

```console
$ make operator-bundle
```

> Note: Push the OCS Bundle image to image registry before moving to next step.

#### OCS Operator Index

An operator index image can then be built using,

```console
$ make operator-index
```

More details on the catalog generation are available in [docs/catalog-generation.md](docs/catalog-generation.md).

### Deploying development builds

To install own development builds of OCS, first build and push the ocs-operator image to your own image repository.

```console
$ export REGISTRY_NAMESPACE=<quay-username>
$ export IMAGE_TAG=<some-tag>
$ make ocs-operator
$ podman push quay.io/$REGISTRY_NAMESPACE/ocs-operator:$IMAGE_TAG
```

Once the ocs-operator image is pushed, edit the CSV to point to the new image.

```console
$ export REGISTRY_NAMESPACE=<quay-username>
$ export IMAGE_TAG=<some-tag>
$ make gen-latest-csv
```

Then build and push the operator bundle image.

```console
$ export REGISTRY_NAMESPACE=<quay-username>
$ export IMAGE_TAG=<some-tag>
$ make operator-bundle
$ podman push quay.io/$REGISTRY_NAMESPACE/ocs-operator-bundle:$IMAGE_TAG
```

Next build and push the operator index image.

```console
$ export REGISTRY_NAMESPACE=<quay-username>
$ export IMAGE_TAG=<some-tag>
$ make operator-index
$ podman push quay.io/$REGISTRY_NAMESPACE/ocs-operator-index:$IMAGE_TAG
```

Now create a namespace and an OperatorGroup for OCS

```console
$ oc create ns openshift-storage

$ cat <<EOF | oc create -f -
apiVersion: operators.coreos.com/v1alpha2
kind: OperatorGroup
metadata:
  name: openshift-storage-operatorgroup
  namespace: openshift-storage
spec:
  targetNamespaces:
    - openshift-storage
EOF
```

Then add a new CatalogSource using the newly built and pushed index image.

```console
$ cat <<EOF | oc create -f -
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: ocs-catalogsource
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: quay.io/$REGISTRY_NAMESPACE/ocs-operator-index:$IMAGE_TAG
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

## Initial Configuration

When the operator starts, it will create a single OCSInitialization resource. That
will cause various initial configuration to be created, including default
StorageClasses.

The OCSInitialization resource is a singleton. If the operator sees one that it
did not create, it will write an error message to its status explaining that it
is being ignored.

### Modifying Initial Configuration

You may modify or delete any of the operator's initial data. To reset and
restore that data to its initial state, delete the OCSInitialization resource. It
will be recreated, and all associated resources will be either recreated or
restored to their original state.

## Functional Tests

Our functional test suite uses the [ginkgo](https://onsi.github.io/ginkgo/) testing framework.

### Prerequisites for running Functional Tests

- OCS must already be installed
- KUBECONFIG env var must be set

### Running functional test

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

### Functional test phases

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

### Running a single test

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

#### Functional test stdout log

This will tell you what test failed and it also outputs some debug information
pertaining to the test cluster's state after the test suite exits. In prow you
can find this log by clicking on the `details` link to the right of the
`ci/prow/ocs-operator-e2e-aws` test entry on your PR. From there you can click
the `Raw build-log.txt` link to view the full log.

#### PROW artifacts

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

## ocs-ci tests

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
