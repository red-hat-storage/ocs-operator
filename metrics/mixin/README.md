# Prometheus Monitoring Mixin for OCS

A set of Prometheus Rules & Alerts for OCS.

The scope of this directory is to provide OCS specific Prometheus rule files using Prometheus Mixin.

## Prerequisites
* Jsonnet [[Install Jsonnet]](https://github.com/google/jsonnet#building-jsonnet)

   [Jsonnet](https://jsonnet.org/learning/getting_started.html) is a data templating language for app and tool developers.

   The mixin directory uses Jsonnet to provide reusable and configurable configs for Prometheus Rules & Alerts.

   * Learning Resources:
      * https://jsonnet.org/learning/tutorial.html
      * https://jsonnet.org/ref/language.html
      * https://github.com/monitoring-mixins/docs
   * Reference Projects:
      * https://github.com/kubernetes-monitoring/kubernetes-mixin
      * https://github.com/openshift/cluster-monitoring-operator/
      * More: https://jsonnet.org/articles/kubernetes.html
* Jsonnet-bundler [[Install Jsonnet-bundler]](https://github.com/jsonnet-bundler/jsonnet-bundler#install)

   [Jsonnet-bundler](https://github.com/jsonnet-bundler/jsonnet-bundler) is a package manager for jsonnet.
* Promtool
  1. [Download](https://golang.org/dl/) Go (>=1.11) and [install](https://golang.org/doc/install) it on your system.
  2. Setup the [GOPATH](http://www.g33knotes.org/2014/07/60-second-count-down-to-go.html) environment.
  3. Run `$ go get -d github.com/prometheus/prometheus/cmd/promtool`


## How to use?

**To get dependencies**

`$ jb install` inside the directory **metrics/mixin/build**

### To add new alerts/rules:

* Add new alerts in 'metrics/mixin/alerts' directory in libsonnet format. Learn about Jsonnet and Libsonnet from https://jsonnet.org/.
  * Example:
    * To create a new alert for notifying if OCS request are above 5 per second in last one minute
      * Create the alert expression using the metric exposed by the ocs exporter
        * Metric exposed : **ocs_exporter_requests_total**
        * Alert expression : **rate(ocs_exporter_requests_total[1m]) > 5**
    * Create a file under metrics/mixin/alerts directory called requests.libsonnet and add the alert in the libsonnet following format.
      *
      ```
      {
         prometheusAlerts+:: {
         groups+: [
            {
               name: 'cluster-request-alert.rules',
               rules: [
               {
                  alert: 'ClusterRequests',
                  expr: |||
                     rate(ocs_exporter_requests_total[1m]) > 5
                  ||| % $._config,
                  'for': $._config.clusterRequestsAlertTime,
                  labels: {
                     severity: 'critical',
                  },
                  annotations: {
                     message: '',
                     description:'',
                     clusterRequestsAlertTime,
                     storage_type: $._config.storageType,
                     severity_level: 'error',
                     runbook_url: 'https://github.com/openshift/runbooks/blob/master/alerts/openshift-container-storage-operator/ClusterRequests.md',
                  },
               },
               ],
            },
         ],
         },
      }

      ```
      * Define constants like clusterRequestsAlertTime, storageType in the metrics/mixin/config.libsonnet file.
      * Double check there is 'runbook' file for document the alert properly. 'runbook_url' annotation will store the link.
      * Add this file to **metrics/mixin/alerts/alerts.libsonnet** or **metrics/mixin/alerts/alerts-external.libsonnet** depending on the type(For internal or external cluster)
    * Test the alert/rule generation by using targets in metrics/mixin/Makefile. Eg:  `make prometheus_alert_rules.yaml`. This is **optional** and can be used to isolate issues.

* Generate deployment manifests by using the OCS-operator Makefile target **gen-latest-prometheus-rules-yamls** : ` make gen-latest-prometheus-rules-yamls `. The generated manifests will be written at metrics/deploy/**prometheus-ocs-rules.yaml**(Internal mode) and metrics/deploy/**prometheus-ocs-rules-external.yaml**(External mode)

* Remember to add a documentation file in the [runbooks OCS folder](https://github.com/openshift/runbooks/tree/master/alerts/openshift-container-storage-operator) with information about the meaning, diagnosis and troubleshooting of the new alert. The new documentation file must follow the same structure other alarms.
The `runbook_url` label in the alert annotations section must point to this new file.

## Background
* [Prometheus Monitoring Mixin design doc](https://docs.google.com/document/d/1A9xvzwqnFVSOZ5fD3blKODXfsat5fg6ZhnKu9LK3lB4/edit#)
