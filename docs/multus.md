Multus is a CNI plugin. Multus allows a Pod to add network interfaces on top of the default network interface. (Note: It does not remove the default interface). Network Attachment Definition is the abstraction used by CNI to define a network interface. The CR is shown below. To use a network interface one would define a NAD (Network Attachment Definition) CR and then would attach it to a [pod][1].

Example storage cluster:
```yaml
apiVersion: ocs.openshift.io/v1
kind: StorageCluster
metadata:
  namespace: openshift-storage
  name: example-storagecluster
spec:
  network:                             
    provider: multus
    selectors:
      public: ocs-public  
      cluster: ocs-cluster
  monPVCTemplate:
    spec:
      storageClassName: gp2-csi
      accessModes:
      - ReadWriteOnce
      resources:
        requests:
          storage: 10Gi
  storageDeviceSets:
  - name: example-deviceset
    count: 3
    resources: {}
    placement: {}
    dataPVCTemplate:
      spec:
        storageClassName: gp2-csi
        accessModes:
        - ReadWriteOnce
        volumeMode: Block
        resources:
          requests:
            storage: 1Ti
    portable: true
```

The selector keys are required to be public and cluster where each represents:
* `public`: client communications with the cluster (reads/writes)
* `cluster`: internal Ceph replication network

Based on the configuration, the operator will do the following:
* If only the public network is specified both communication and replication will happen on that network
* If both public and cluster networks are specified the first one will run the communication network and the second the replication network

In order to work, each network key value must match a `NetworkAttachmentDefinition` object name in Multus. 

For multus network provider, an already working cluster with Multus networking is required. Network attachment definition that later will be attached to the cluster needs to be created before the Cluster CRD. If Rook cannot find the provided Network attachment definition it will fail running the Ceph OSD pods. You can add the Multus network attachment selection annotation selecting the created network attachment definition on selectors.

The following are the examples of valid multus NetworkAttachmentDefinitions:
Create a `public` and `cluster` network by creating a `NetworkAttachmentDefinition` object.
Example NetworkAttachmentDefinition
```yaml
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: ocs-public
  namespace: openshift-storage
spec:
  config: '{
      "cniVersion": "0.3.0",
      "type": "macvlan",
      "master": "ens3",
      "mode": "bridge",
      "ipam": {
            "type": "whereabouts",
            "range": "192.168.1.0/24",
      }
  }'
```
```yaml
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: ocs-cluster
  namespace: openshift-storage
spec:
  config: '{
      "cniVersion": "0.3.0",
      "type": "macvlan",
      "master": "ens3",
      "mode": "bridge",
      "ipam": {
            "type": "whereabouts",
            "range": "192.168.0.0/24",
      }
  }'
```
> NOTE: In this example the master field must reference a network interface that resides on the node(s) hosting the Pod(s). So as per this example we have an “ens3” interface on all the nodes.
More information on NetworkAttachmentDefinition can be found [here][2].

[1]: https://docs.openshift.com/container-platform/4.4/networking/multiple_networks/attaching-pod.html
[2]: https://docs.openshift.com/container-platform/4.1/networking/managing-multinetworking.html