# Custom Resource Metrics Exporter

# Summary

Custom Resource Metrics Exporter provides an insight into the OpenShift Container Storage (OCS) StorageCluster by aggregating data from various Custom Resources
and exposing metrics about it. The exposed metrics will be in Prometheus compliant format and can be used to build alerts and charts.

# Motivation

The main motivation behind this exporter is to improve user experience for the consumers of OCS. Currently, cluster state and other custom resource related information can only be found by digging deeper into multiple layers via CLI or Console.

To improve that, this exporter will surface out those details in the form of metrics which can then be consumed by Prometheus for analysis as required. Alerts will be created on top of that to notify the user.

# Proposal

The exporter will watch for events on desired Custom Resources and Namespaces and create meaningful metrics with labels. The metric type will be one of Counters, Gauge, Histogram or Summary. Help text and description will be provided along with the metrics for easy understanding.

For example, we will provide RGW health check information based on the CephObjectStore CR. Required parts of the Custom Resource spec is presented below.

```YAML
apiVersion: ceph.rook.io/v1
kind: CephObjectStore
metadata:
  name: default
  namespace: openshift-storage
spec:
  ...
  gateway:
    ...
    externalRgwEndpoints:
      - ip: 10.70.56.202
  ...
status:
  bucketStatus:
    details: >-
      failed to create object user
      "rook-ceph-internal-s3-user-checker-9e599ac7-229c-43d4-8e23-40974c9e9177".
      error code 1 for object store "default": failed to create s3 user: exit
      status 5
    health: Failure
    lastChecked: '2020-07-16T19:20:58Z'
  info:
    endpoint: 'http://rook-ceph-rgw-default.openshift-storage:8080'
  phase: Ready
```

Name field will be added to label so that the users can uniquely identify the metrics and corelate to historical data. Being an ID, it is unbounded and can have high cardinality.

Namespace field can have many values depending on the number of namespaces in the cluster. To limit this value, only `openshift-storage` will be used by default and users can enable watching more namespaces using flags.

Health field will have a pre-determined set of values. All these values are considered for label.

Based on these data, we create a Prometheus Gauge metric `ocs_rgw_health_status` whose value will be 0(Healthy) or 1(Failure).
Labels will be added to provide context as described above.

The exposed metrics will look like:

```
ocs_rgw_health_status {name="default", namespace="openshift-storage",  rgw_endpoint="http://rook-ceph-rgw-default.openshift-storage:8080", status="Failure"} 1
```

# Goals

+ Expose Prometheus metrics from Custom Resources.
+ Expose Prometheus metrics about the exporter itself.
+ Allow configuration via arguments (like setting log level and server port).
+ Add basic logging.
+ Allow exporter to run in-cluster or standalone.
  + in-cluster runs in a pod inside the cluster.
  + outside the cluster, exporter can run standalone and can monitor the cluster based on provided cluster config.
    Prometheus can be configured to scrape from this exporter.
+ Add unit and E2E tests.
+ Provide deployment manifests for ease fo use.

# Implementation Details

+ In order to efficiently monitor the Custom Resources, Listers and Watchers from Kubernetes `client-go` package will be used. 
+ To create Prometheus metrics out of these events, `Prometheus client-go` will be used.
+ HTTP Server and handlers will be created using `net/http` and `promhttp` package.

Custom Collectors will be created to list the different metrics that should be collected and then added to a Prometheus Registry to be exposed by the exporter. This collector will implement the `prometheus.Collector` interface,

+ `collector.Describe()` will provide the help text about the metrics.
+ `collector.Collect()` will be used by Prometheus for scraping the metrics.

## Deployment

There are different possible ways for deploying metrics exporter to the Kubernetes/OpenShift cluster.

+ standalone : run the exporter executable and point it to the apiserver for the cluster which needs to be monitored. Curl the `<host:port/metrics>` endpoint to receive the exported metrics.
+ in-cluster
  + separate deployment : use the example manifests to create a new deployment for the exporter. Expose the exporter pod to a service and use it to curl exported metrics.
  + attached deployment : exporter will be deployed attached to the OCS operator pod and service will be exposed for the metrics endpoint.