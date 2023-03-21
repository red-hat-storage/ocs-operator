OCS must-gather
=================

`ocs-must-gather` is a tool built on top of [OpenShift must-gather](https://github.com/openshift/must-gather)
that expands its capabilities to gather Openshift Container Storage for information.

### Usage
```sh
oc adm must-gather --image=quay.io/ocs-dev/ocs-must-gather
```

The command above will create a local directory with a dump of the ocs state.
Note that this command will only get data related to the ocs part of the OpenShift cluster.

You will get a dump of:
- The OCS Operator namespaces (and its children objects)
- All namespaces (and their children objects) that belong to any OCS resources
- All OCS CRD's definitions
- All namespaces that contains ceph and noobaa
- Output of the following ceph commands
    ```
    ceph status
    ceph health detail
    ceph osd tree
    ceph osd stat
    ceph osd dump
    ceph mon stat
    ceph mon dump
    ceph df
    ceph report
    ceph osd df tree
    ceph fs dump
    ceph fs ls
    ceph pg dump
    ceph health detail
    ceph osd crush show-tunables
    ceph osd crush dump
    ceph mgr dump
    ceph mds stat
    ceph versions
    ```

In order to get data about other parts of the cluster (not specific to OCS) you should
run `oc adm must-gather` (without passing a custom image). Run `oc adm must-gather -h` to see more options.

### How to Contribute

#### Contribution Flow
Developers must follow these steps to make a change:
1. Fork the `red-hat-storage/ocs-operator` repository on GitHub.
2. Create a branch from the master branch, or from a versioned branch (such
   as `release-4.2`) if you are proposing a backport.
3. Make changes.
4. Ensure your commit messages include sign-off.
5. Push your changes to a branch in your fork of the repository.
6. Submit a pull request to the `red-hat-storage/ocs-operator` repository.
7. Work with the community to make any necessary changes through the code
   review process (effectively repeating steps 3-7 as needed).

#### Commit and Pull Request Messages

- Refer and Follow same standards mention in [OCS-Operator How to Contribute](./../CONTRIBUTING.md)
- Tag the Pull Request with `must-gather`
