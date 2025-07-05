# Table of Contents

- [OpenShift Container Storage Operator](#openshift-container-storage-operator)
- [Installation](#installation)
  - [Prerequisites](#prerequisites)
    - [Dedicated nodes](#dedicated-nodes)
  - [Installing with OLM](#installing-with-olm)
  - [Creating StorageCluster](#creating-storagecluster)
- [Development](#development)
  - [Tools](#tools)
  - [Build](#build)
    - [OCS Operator](#ocs-operator)
    - [OCS Metric Exporter](#ocs-metric-exporter)
    - [OCS Operator Bundle](#ocs-operator-bundle)
  - [Deploying development builds](#deploying-development-builds)
- [Functional Tests](#functional-tests)
  - [Prerequisites for running Functional Tests](#prerequisites-for-running-functional-tests)
  - [Running functional test](#running-functional-test)
  - [Functional test phases](#functional-test-phases)
  - [Developing Functional Tests](#developing-functional-tests)
  - [Running a single test](#running-a-single-test)
  - [Debugging Functional Test Failures](#debugging-functional-test-failures)
    - [Functional test stdout log](#functional-test-stdout-log)
    - [PROW artifacts](#prow-artifacts)
    - [Getting live access to the PROW CI cluster](#getting-live-access-to-the-prow-ci-cluster)

## OpenShift Container Storage Operator

This is a "meta" operator, meaning it serves to facilitate the other operators in
OCS by performing administrative tasks outside their scope as well as
watching and configuring their CustomResources (CRs).

## Installation

### Prerequisites

OCS Operator will install its components only on nodes labelled for OCS with `cluster.ocs.openshift.io/openshift-storage=''`.

To label the nodes from CLI,

```console
$ oc label nodes <NodeName> cluster.ocs.openshift.io/openshift-storage=''
```

OCS requires at least 3 nodes labelled this way.

> Note: When deploying via Console, the creation wizard takes care of labelling the selected nodes.

#### Dedicated nodes

In case dedicated storage nodes are available, these can also be tainted to allow only OCS components to be scheduled on them.
Nodes need to be tainted with `node.ocs.openshift.io/storage=true:NoSchedule` which can be done from the CLI as follows,

```console
$ oc adm taint nodes <NodeNames> node.ocs.openshift.io/storage=true:NoSchedule
```

> Note: The dedicated/tainted nodes will only run OCS components. The nodes will not run any apps. Therefore, if you taint, you need to have additional worker nodes that are untainted. If you don't, you will be unable to run any other apps in you OpenShift cluster.

### Installing with OLM

The OCS operator can be installed into an OpenShift cluster using Operator Lifecycle Manager (OLM).

If you have a development environment or private image and want to install the OCS operator, follow the steps below:

- Set Environment Variables:  
  Define the required variables for your image:

  ```console
  export REGISTRY_NAMESPACE=<your-registry-namespace>
  export IMAGE_TAG=<your-image-tag>
  ```

- Run the following command:  
  This installs all the dependencies of ocs-operator and the ocs-operator bundle using the operator-sdk cli

  ```console
  make install
  ```

- Verify Installation:    
  Once the make install process completes, verify the status of the ClusterServiceVersion (CSV):

  ```console
  oc get csv -n openshift-storage
  ```

### Creating StorageCluster

A StorageCluster can be created using the example CR as follows,

```console
$ oc create -f ./deploy/storagecluster.yaml
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

#### OCS Metric Exporter

The metric exporter image can be built via:

```console
$ make ocs-metrics-exporter
```

#### OCS Operator Bundle

To create an operator bundle image, run

```console
$ make operator-bundle
```

> Note: Push all the images built to image registry before moving to next step.

### Deploying development builds

Install the build by following the steps from the [Installation](#installation) section.

## Functional Tests

Our functional test suite uses the [ginkgo](https://onsi.github.io/ginkgo/) testing framework.
The ginkgo `functests` test suite in this repo is for developers. As new
functionality is introduced into the ocs-operator, this repo allows developers
to prove their functionality works by including tests within their PR. This is
the test suite where we exercise ocs-operator deployment/update/uninstall as
well as some basic workload functionality like creating PVCs.

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
`ci/prow/ocs-operator-bundle-e2e-aws` test entry on your PR. From there you can click
the `Raw build-log.txt` link to view the full log.

#### PROW artifacts

In addition to the raw test stdout, each e2e test result has a set of artifacts
associated with it that you can view using prow. These artifacts let you
retroactively view information about the test cluster even after the e2e job
has completed.

To browse through the e2e test cluster artifacts, click on the `details` link
to the right of the `ci/prow/ocs-operator-bundle-e2e-aws` test entry on your PR. From
there look at the top right hand corner for the `artifacts` link. That will
bring you to a directory tree. Follow the `artifacts/` directory to the
`ocs-operator-bundle-e2e-aws/` directory. There you can find logs and information
pertaining to objects in the cluster.

#### Getting live access to the PROW CI cluster

Click on the job link for the e2e test (ci/prow/ocs-operator-bundle-e2e-aws). In the initial log lines, look for the "Using namespace" line which includes the OpenShift console URL; open that URL. In the OpenShift console, go to the "Overview" tab and navigate to the "Secrets" section. Find the secret named ocs-operator-bundle-e2e-aws. Inside the secret, locate the key KUBECONFIG and use its value to access the cluster.

> Note: Only the author of the PR on which the tests are running can login to the console URL to get the KUBECONFIG. The cluster is torn down after the tests complete, so you must be proactive in accessing it while the tests are still running.

