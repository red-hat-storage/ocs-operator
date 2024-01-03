## How to Contribute

The OCS-Operator project is under the [Apache 2.0 license](LICENSE). We accept
contributions via GitHub pull requests. This document outlines how to
contribute to the project.

## Contribution Flow

Developers must follow these steps to make a change:

1. Fork the `red-hat-storage/ocs-operator` repository on GitHub.
2. Create a branch from the master branch, or from a versioned branch (such
   as `release-4.2`) if you are proposing a backport.
3. Create the [Design doc](#design-doc) and get it approved before making any
   changes to the actual code.
4. Make changes.
5. Create tests as needed and ensure that all tests pass.
6. Ensure your commit messages include sign-off.
7. Push your changes to a branch in your fork of the repository.
8. Submit a pull request to the `red-hat-storage/ocs-operator` repository.
9. Work with the community to make any necessary changes through the code
   review process (effectively repeating steps 3-8 as needed).

## Developer Environment Installation

Instructions to create a dev environment for OCS-operator can be found in the
[main project documentation](./README.md#installation-of-development-builds).

## Commit Requirements

In addition to passing [functional tests](./README.md#functional-tests), all
commits to OCS-operator must follow the guidelines in this section. You can
verify your changes meet most of these standards by running the following
command in the repository's root directory:

```
make ocs-operator-ci
```

### Commits Per Pull Request

*This requirement cannot be tested by `make ocs-operator-ci`.*

OCS-operator is a project which maintains several versioned branches
independently. When backports are necessary, monolithic commits make it
difficult for maintainers to cleanly backport only the necessary changes.

Pull requests should always represent a complete logical change. Where
possible, though, pull requests should be composed of multiple commits that
each make small but meaningful changes. Striking a balance between minimal
commits and logically complete changes is an art as much as a science, but
when it is possible and reasonable, divide your pull request into more commits.

Some times when it will almost always make sense to separate parts of a change
into their own commits are:
- Auto generated changes for example vendor, bundle etc.
- Changes to unrelated formatting and typo-fixing.
- Refactoring changes that prepare the codebase for your logical change.
- Similar changes to multiple parts of the codebase (for instance, similar
  changes to handling of CephFilesystem, CephBlockPool, and CephObjectStore).

Even when breaking down commits, each commit should leave the codebase in a
working state. The code should add necessary unit tests and pass unit tests,
formatting tests, and usually functional tests. There can be times when
exceptions to these requirements are appropriate (for instance, it is sometimes
useful for maintainability to split code changes and related changes to CRDs
and CSVs). Unless you are very sure this is true for your change, though, make
sure each commit passes CI checks as above.

### Commit and Pull Request Messages

*This requirement cannot be fully tested by make `ocs-operator-ci`.*

- The message must begin with a single summary line describing the change.
  - It must be capitalized.
  - It must not end with a period.
  - It must be <= 72 characters in length.
  - *Recommendation*: It should be written in the *imperative mood*: "Fix bug x"
    rather than "Fixed bug x" or "Fixes bug x". (The sentence "If applied, this
    commit will \<your summary\>" should be grammatical.)
- The message should continue with a longer description of the change.
  - The description must be preceded by a single blank line to separate it from
    the summary.
  - It must describe why the change is necessary or useful.
  - It must describe how the change was implemented.
  - It must reference any open Issues the change addresses.
  - It must wrap at 80 characters.
  - *For very small changes it may be acceptable to omit the longer description.
    Please remember, that it is easy for any developer to believe their code is
    "self-documenting" when it is not. Adding a description will help future
    maintainers of the project.*
- The message must end with a sign-off, as discussed in [Certificate of
  Origin](#certificate-of-origin).
  - The sign-off must be preceded by a single blank line to separate it from
    the preceding section.
  - If the code has multiple authors:
     - Each author must add a sign-off.
     - *Recommendation*: A "Co-authored-by:" line should be added for each
       additional author.

Pull request messages should follow the same format, but for the entire set of
changes contained in the pull request.

### Design Doc

Design Docs essentials:
- Justification for the change.
- What are the potential approaches?
- Why we picked one over the others?

### Coding Style

All changes must follow style guidelines tested by the `golangci/golangci-lint`
project. You can verify that your code meets these standards specifically by
running:

```
make golangci-lint
```

### Unit tests

Changes should be covered by unit tests and all unit tests must pass. These
can be run directly by running:

```
make unit-test
```

*It is of special note that many of the mock objects in the [StorageCluster
tests](./pkg/controller/storagecluster/storagecluster_controller_test.go) are
reused in many other test cases. Please avoid using the global mock variables
directly, instead use the mock variables by creating DeepCopy() to prevent
modification of global mock variables. They may be useful in developing your
own.*

### Certificate of Origin

*This requirement is not currently tested by `make ocs-operator-ci`.*

All commit messages must contain a sign-off indicating that the contributor
asserts that they have the legal right to make the contribution and agrees
that the contribution will be a matter of public record. See [DCO](./DCO) for
legal details. An example of this sign-off is:

```
A description of my change

Signed-off-by: Your Name Here <youremailaddress@yourdomain.org>
```

Git has added the `-s` flag to `git commit`, which will automatically
append the necessary signature:

```
git commit -s
```

If you need to add a signoff to your latest commit, append the `--amend` flag
as well.
