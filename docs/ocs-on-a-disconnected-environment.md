# OCS on a Disconnected environment

This doc is based on [this document][1]

## Motivation
In a disconnected environment there is no access to the OLM catalog and the image registries, so in order to install OCS we need to do 2 things:
1. provide a custom catalog that contains OCS CSV (cluster service version)
    - this is done using the command `oc adm catalog build`. this command is going over a given catalog (e.g. redhat-operators), builds an olm catalog image and pushes it to the mirror registry.
    - to work around the issue of `lib-bucket-provisioner` which is a dependency in community-operators we will need to build a catalog image for community-operators as well. once this issue will be fixed this will be unnecessary.
2. mirror all images that are required by OCS to a mirror registry which is accessible from the OCP cluster
    - this is done using the command `oc adm catalog mirror`. this command goes over the CSVs in the catalog and copy all required images to the mirror registry.
    - to work around the missing `relatedImages` in OCS CSV, we will need to manually mirror required images which are not copied with `oc adm catalog mirror`. this is done using `oc image mirror`
    - `oc adm catalog mirror` generates `imageContentSourcePolicy.yaml` to install in the cluster. this resource tells OCP what is the mapping each image in the mirror registry. we need to add to it also the mapping of the related images before aplying in the cluster.



## prerequisites
1. assuming that a disconnected cluster is already installed and a mirror registry exists on a bastion host ([see here][4]).   
The following steps can also be applied and tested on a connected cluster. since we disable the default catalog (operator hub), the flow should be similar on both connected and disconnected envs
2. oc is installed and logged in to the cluster. I used [oc 4.4][5], for earlier versions thigs might not work as well


## env vars (fill the correct details for your setup)
```
export AUTH_FILE="~/podman_config.json"
export MIRROR_REGISTRY_DNS="yydev2.mirror-registry.qe.gcp.devcluster.openshift.com:5000"
```


## create auth file
podman login to mirror registry and generate authfile 
```
podman login ${MIRROR_REGISTRY_DNS} --tls-verify=false --authfile ${AUTH_FILE}
```
get redhat registry [pull secret][3] and merge the auth with `${AUTH_FILE}`
you should get something similar to this:
```json
{
    "auths": {
        "cloud.openshift.com": {
            "auth": "*****************",
            "email": "user@redhat.com"
        },
        "quay.io": {
            "auth": "*****************",
            "email": "user@redhat.com"
        },
        "registry.connect.redhat.com": {
            "auth": "*****************",
            "email": "user@redhat.com"
        },
        "registry.redhat.io": {
            "auth": "*****************",
            "email": "user@redhat.com"
        },
        "yydev2.mirror-registry.qe.gcp.devcluster.openshift.com:5000": {
            "auth": "*****************",
        }
    }
}
```


## Building and mirroring the Operator catalog image

- **build oeprators catalog for redhat operators**

```
oc adm catalog build --insecure --appregistry-endpoint https://quay.io/cnr --appregistry-org redhat-operators --to=${MIRROR_REGISTRY_DNS}/test-restricted/redhat-operators:v1 --registry-config=${AUTH_FILE}
```

- **build oeprators catalog for community operators** (required as workaround for lib-bucket-provisioner dependency)

```
oc adm catalog build --insecure --appregistry-endpoint https://quay.io/cnr --appregistry-org community-operators --to=${MIRROR_REGISTRY_DNS}/test-restricted/community-operators:v1 --registry-config=${AUTH_FILE}
```

- **Disable the default OperatorSources by adding disableAllDefaultSources: true to the spec**
```
oc patch OperatorHub cluster --type json -p '[{"op": "add", "path": "/spec/disableAllDefaultSources", "value": true}]'
```

- **mirror the redhat-operators catalog**  
This is a long operation and should take ~1-2 hours. It requires ~20 GB of disk space on the bastion (the mirror machine)
```
oc adm catalog mirror ${MIRROR_REGISTRY_DNS}/test-restricted/redhat-operators:v1 ${MIRROR_REGISTRY_DNS}  --insecure --registry-config=${AUTH_FILE}
```

- **Manually Add the related images to the imageContentSourcePolicy**  
After the mirror is completed it will print an output dir where an `imageContentSourcePolicy.yaml` is generated. 
we need to append the the related images to the file. e.g: 
```yaml
  - mirrors:
    - yydev2.mirror-registry.qe.gcp.devcluster.openshift.com:5000/ocs4/rhceph-rhel8
    source: registry.redhat.io/ocs4/rhceph-rhel8
  - mirrors:
    - yydev2.mirror-registry.qe.gcp.devcluster.openshift.com:5000/ocs4/mcg-core-rhel8
    source: registry.redhat.io/ocs4/mcg-core-rhel8
  - mirrors:
    - yydev2.mirror-registry.qe.gcp.devcluster.openshift.com:5000/openshift4/ose-csi-driver-registrar
    source: registry.redhat.io/openshift4/ose-csi-driver-registrar
  - mirrors:
    - yydev2.mirror-registry.qe.gcp.devcluster.openshift.com:5000/openshift4/ose-csi-external-attacher
    source: registry.redhat.io/openshift4/ose-csi-external-attacher
  - mirrors:
    - yydev2.mirror-registry.qe.gcp.devcluster.openshift.com:5000/ocs4/cephcsi-rhel8
    source: registry.redhat.io/ocs4/cephcsi-rhel8
  - mirrors:
    - yydev2.mirror-registry.qe.gcp.devcluster.openshift.com:5000/openshift4/ose-csi-external-provisioner-rhel7
    source: registry.redhat.io/openshift4/ose-csi-external-provisioner-rhel7
  - mirrors:
    - yydev2.mirror-registry.qe.gcp.devcluster.openshift.com:5000/ocs4/mcg-rhel8-operator
    source: registry.redhat.io/ocs4/mcg-rhel8-operator
  - mirrors:
    - yydev2.mirror-registry.qe.gcp.devcluster.openshift.com:5000/rhscl/mongodb-36-rhel7
    source: registry.redhat.io/rhscl/mongodb-36-rhel7
```


- **After adding the related images, apply this file to the cluster**
```
oc apply -f ./[output dir]/imageContentSourcePolicy.yaml
```


- **Manually mirror related images**  
As a workaround for the missing `relatedImages` in ocs CSV we want to mirror operands images manually.  
`oc image mirror` accepts as input a mapping file.  
the format of a mapping is `registry.redhat.io/account/repository@sha256:xxxxxx=mirror.registry/account/repository` (no tag at the target)  
below is a mapping file for OCS 4.2 (other versions will reqiure different images. go over the CSV and make sure that all of the used images are mapped in the file)  
save the content below to `mapping.txt` 
```
registry.redhat.io/ocs4/cephcsi-rhel8@sha256:280b39004dc8983eba810f6707b15da229efd62af512839a4f14efc983df2110=yydev2.mirror-registry.qe.gcp.devcluster.openshift.com:5000/ocs4/cephcsi-rhel8
registry.redhat.io/ocs4/mcg-core-rhel8@sha256:c717c19981446afc2c17460fa891c1a1fcb39be497b1bea3f86e87bb5e6c2305=yydev2.mirror-registry.qe.gcp.devcluster.openshift.com:5000/ocs4/mcg-core-rhel8
registry.redhat.io/ocs4/rhceph-rhel8@sha256:8f51e6b6ca3017eeebd57026fbf1f9f74831a18d13f605fac1b9694dd41e55cf=yydev2.mirror-registry.qe.gcp.devcluster.openshift.com:5000/ocs4/rhceph-rhel8
registry.redhat.io/openshift4/ose-csi-driver-registrar@sha256:797ff087ed64e38d0de12875f58ea395ec222bf67cad59a6abb6c131e12bfb97=yydev2.mirror-registry.qe.gcp.devcluster.openshift.com:5000/openshift4/ose-csi-driver-registrar
registry.redhat.io/openshift4/ose-csi-external-attacher@sha256:72fcb2c114b7a4b0222802d4fe82ada7620162b1120462942c8a9d8e66230677=yydev2.mirror-registry.qe.gcp.devcluster.openshift.com:5000/openshift4/ose-csi-external-attacher
registry.redhat.io/openshift4/ose-csi-external-provisioner-rhel7@sha256:203417dc512f0e3583be06db7f93a3729fef08d8e939ec1fc828ef914d1d8995=yydev2.mirror-registry.qe.gcp.devcluster.openshift.com:5000/openshift4/ose-csi-external-provisioner-rhel7
registry.redhat.io/rhscl/mongodb-36-rhel7@sha256:ad5dc22e6115adc0d875f6d2eb44b2ba594d07330a600e67bf3de49e02cab5b0=yydev2.mirror-registry.qe.gcp.devcluster.openshift.com:5000/rhscl/mongodb-36-rhel7
```

- **Mirror the images in `mapping.txt`**  
```
oc image mirror -f mapping.txt --insecure --registry-config=${AUTH_FILE}
```

- **Create CatalogSource for redhat-operators**  
Create a CatalogSource object that references the catalog image for redhat-operators. Modify the following to your specifications and save it as a catalogsource.yaml file:
```yaml
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: redhat-operators-catalog
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: yydev2.mirror-registry.qe.gcp.devcluster.openshift.com:5000/test-restricted/redhat-operators:v1 
  displayName: Redhat Operators Catalog
  publisher: grpc
```
create the catalog source:
```
oc create -f catalogsource.yaml
```

- **repeat for community operators**
```yaml
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: community-operators-catalog
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: yydev2.mirror-registry.qe.gcp.devcluster.openshift.com:5000/test-restricted/community-operators:v1 
  displayName: Community Operators Catalog
  publisher: grpc
```

All Done!



the ***Operators*** section in the UI should now present all of the catalog content, and you can install OCS from the mirror registry

[1]:https://docs.openshift.com/container-platform/4.3/operators/olm-restricted-networks.html#olm-restricted-networks-operatorhub_olm-restricted-networks
[2]: https://docs.openshift.com/container-platform/4.3/operators/olm-restricted-networks.html#olm-building-operator-catalog-image_olm-restricted-networks
[3]: https://cloud.redhat.com/openshift/install/pull-secret
[4]: https://access.redhat.com/documentation/en-us/openshift_container_platform/4.3/html/installing/installation-configuration#installing-restricted-networks-preparations
[5]: https://mirror.openshift.com/pub/openshift-v4/clients/ocp-dev-preview/latest-4.4/
