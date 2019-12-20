OCS operator makes use of the [OpenShift-CI][1] platform for CI and automation of project lifecycle activities.
The Openshift-CI configuration for ocs-operator lives in [openshift/release][2] repository.

> NOTE: The commands and path given in this document are all relative to the openshift/release repository.

For any help with the openshift-ci, reach out to the maintainers in `#forum-testplatform` on the CoreOS Slack.


## Test configs

The ocs-operator test configs live under `ci-operator/config/openshift/ocs-operator` in the openshift/release repository.
Each branch of the ocs-operator repository has its own configuration file, and is named `openshift-ocs-operator-${branch}.yaml`.
The config file format reference is available in [CONFIGURATION.md][4].
A new config file needs to be created by copying and editing the master config whenever a new release branch is created.
The master config should then be updated for the next release.

These files are used to generate the [Prow][3] job configs under `ci-operator/jobs/openshift/ocs-operator`. After each modification of the config files run

```console
$ make jobs
```

to generate/update the job files.

Individual pre-submit, post-submit and periodic job files are generated for each branch.
The periodic job files are only generated if a periodic test has been defined in a test config.
The job files should not be manually edited, except to mark a job as optional.

### Test suites

Currently, 3 tests are defined in the configs for ocs-operator.

1. `ocs-operator-ci` - the general smoke test that verifies the PR. This test is required for PRs to be merged.
2. `ocs-operator-e2e-aws` - the ocs-operator functional test suite. This test is required for PRs to be merged. This test is run on a new OCP cluster that is created by openshift-ci on AWS.
3. `red-hat-storage-ocs-ci-e2e-aws` - the functional tests from [red-hat-storage/ocs-ci][5]. This test is run for each PR, but is not required for merge. This test is run on a new OCP cluster that is created by openshift-ci on AWS.

Each of the above tests generates a job in the pre-submit job files.

### Images

The config files also define various images that will be generated for consumption during the tests. A pre-submit job is generated that builds the images required for testing. An additional post-submit job is also generated that builds and promotes images to be mirrored.

Several different types of images are defined.

#### Base images

These are the images that would be used as the base for the later images. The ci-operator will make these available in the test namespace.

#### Source and Binary images

The ci-operator builds the base source and binary images from the build root image. The build root image is built from `openshift-ci/Dockerfile.tools` in the ocs-operator repo.
The source image is the build root with the ocs-operator source copied into it, and the binary image is the source image with the ocs-operator binaries built.

#### Test images

The test images are used during the e2e tests. Test images are built for ocs-operator and ocs-registry. The ocs-registry test image, contains a CSV that points to the test ocs-operator image. They use different dockerfiles in `openshift-ci/` instead of the normal dockerfiles used for public images.

#### Mirrored images

Seperate images are built to be mirrored to the [quay.io/ocs-dev][6] public repositories. These images are suffixed with `-quay`. These images are built from the normal dockerfiles. The mirroring of these images is described later in the document.

### Manual execution

The [ci-operator][7], which runs these tests in openshift-ci cluster, can also be run manually to check if the tests have been defined correctly. The ci-operator can be built manually from the [ci-tools][8] repository.

#### Normal tests

Normal single container tests, ie. the non end-to-end tests, can be run on any OpenShift cluster. Either `oc login` to your own cluster or export `KUBECONFIG` for your own cluster, and run

```console
$ ci-operator --config <path-to-config-file> --git-ref openshift/ocs-operator@<gitref> --target <test-to-run>
```

For eg.,

```console
$ ci-operator --config ci-operator/config/openshift/ocs-operator/openshift-ocs-operator-master.yaml --git-ref openshift/ocs-operator@master --target ocs-operator-ci
```

#### E2E tests

End-to-end tests require additional setup in the OCP cluster being targetted, to allow creation of new OCP cluster. Since this is not easy, [api.ci.openshift.org][1] the openshift-ci cluster can be used instead.

Login to [api.ci.openshift.org][1] web-console using the Github login option and obtain the CLI login command. Only members of the Github [OpenShift][9] can login to the cluster, so ensure you are a member.

Login to the cluster using the obtained login command, and run

```console
$ ci-operator --config <path-to-config-file> --git-ref openshift/ocs-operator@<gitref> --target <test-to-run> --template <path-to-cluster-template>
```

For eg.,

```console
$ ci-operator --config ci-operator/config/openshift/ocs-operator/openshift-ocs-operator-master.yaml --git-ref openshift/ocs-operator@master --target ocs-operator-e2e-aws --template ci-operator/templates/openshift/installer/cluster-launch-installer-src.yaml
```

## Automation

### PR automation

PRs on ocs-operator are automatically merged, provided tests have passed and labels are correct, using [Tide][10].
The Tide configuration for ocs-operator is present in the `core-services/prow/02_config/_config.yaml` file.
The Tide configuration should be updated whenever a new release branch is created.
The Tide configuration structure is documented [here][14].

### Bugzilla automation

The Prow bugzilla plugin is used to verify BZs attached to PRs on the release branches.
The ocs-operator config is present in `core-service/prow/02_config/_plugins.yaml` file.
The bugzilla plugin configuration should be updated whenever a new release branch is created.
The bugzilla plugin configuration structure is documented [here][15].

## Image mirroring

The images built and promoted by the post-submit jobs, are mirrored to [quay.io/ocs-dev][6] using the [image mirroring service][11].
The ocs-operator image mirroring mapping is defined in the `core-services/image-mirroring/ocs-operator/mapping_ocs-operator_quay` file.
This mapping should be updated whenever a release branch is created.
A periodic image mirroring job is also setup for ocs-operator in `ci-operator/jobs/infra-image-mirroring.yaml`.

### Push secret

The push secret is made available using the [secret mirroring][12] service. The ocs-operator mapping is present in `core-services/secret-mirroring/_mapping.yaml`.

The push secret is defined in the `ci-ocs-operator-secrets` project in api.ci.openshift.org, as `ocs-operator-quay-secret`.

#### Creating the secret in the secret namespace

Ensure that the secret namespace, `ocs-operator-quay-secret` is created.

Get the dockercfg json file for the bot account from [ocs-operator robot accounts][12] on quay.io. Create a bot account if needed.

Get the pull token for `registry.api.ci.openshift.org` from one of the dockercfg secrets in the secret namespace. Add this token to the dockercfg file for the bot account. Without this the image mirroring job will not be able to pull the promoted images for mirroring.

The new json file should be similar to

```json
{
  "auths": {
    "quay.io": {
      "auth": "XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX......",
      "email": ""
    },
    "registry.svc.ci.openshift.org": {
      "username": "serviceaccount",
      "password": "XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX......",
      "email": "serviceaccount@example.org",
      "auth": "XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX......"
    }
  }
}
```

Name this file `config.json` and create the secret with

```console
$ oc -n ci-ocs-operator-secrets create secret generic ocs-operator-quay-secret --from-file=config.json
```

If the name of the secret or the file is changed, the infra mirroring job needs to be updated as well.

[1]: https://api.ci.openshift.org
[2]: https://github.com/openshift/release
[3]: https://github.com/kubernetes/test-infra/tree/master/prow
[4]: https://github.com/openshift/ci-tools/blob/master/CONFIGURATION.md
[5]: https://github.com/red-hat-storage/ocs-ci
[6]: https://quay.io/ocs-dev
[7]: https://github.com/openshift/ci-tools/blob/master/CI_OPERATOR.md
[8]: https://github.com/openshift/ci-tools
[9]: https://github.com/openshift
[10]: https://github.com/kubernetes/test-infra/tree/master/prow/tide
[11]: https://github.com/openshift/release/blob/master/core-services/image-mirroring/README.md
[12]: https://github.com/openshift/release/blob/master/core-services/secret-mirroring/README.md
[13]: https://quay.io/organization/ocs-dev?tab=robots
[14]: https://godoc.org/github.com/kubernetes/test-infra/prow/config#Tide
[15]: https://godoc.org/k8s.io/test-infra/prow/plugins#BugzillaBranchOptions
