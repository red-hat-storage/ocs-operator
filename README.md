# The OCS Meta Operator

This is the primary operator for Red Hat OpenShift Container Storage (OCS). It
is a "meta" operator, meaning it serves to facilitate the other operators in
OCS by performing administrative tasks outside their scope as well as
watching and configuring their CustomResources (CRs).

## Build

The operator is based on the [Operator
SDK](https://github.com/operator-framework/operator-sdk). In order to build the
operator, you first need to install the SDK. [Instructions are
here.](https://github.com/operator-framework/operator-sdk#quick-start)

Once the SDK is installed, the operator can be built via:

```bash
$ dep ensure --vendor-only

$ operator-sdk build quay.io/openshift/ocs-operator
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
