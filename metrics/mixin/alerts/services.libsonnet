{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'cluster-services-alert.rules',
        rules: [
          {
            alert: 'ClusterObjectStoreState',
            expr: |||
              ocs_rgw_health_status{%(ocsExporterSelector)s} == 2
              or
              kube_deployment_status_replicas_ready{deployment=~"rook-ceph-rgw-.*"} < kube_deployment_spec_replicas{deployment=~"rook-ceph-rgw-.*"}
            ||| % $._config,
            'for': $._config.clusterObjectStoreStateAlertTime,
            labels: {
              severity: 'critical',
            },
            annotations: {
              message: 'Cluster Object Store is in unhealthy state or number of ready replicas for Rook Ceph RGW deployments is less than the desired replicas in namespace:cluster {{ $labels.namespace }}:{{ $labels.managedBy }}.',
              description: 'RGW endpoint of the Ceph object store is in a failure state or one or more Rook Ceph RGW deployments have fewer ready replicas than required for more than %s. Please check the health of the Ceph cluster and the deployments in namespace:cluster {{ $labels.namespace }}:{{ $labels.managedBy }}.' % $._config.clusterObjectStoreStateAlertTime,
              storage_type: $._config.objectStorageType,
              severity_level: 'error',
            },
          },
        ],
      },
    ],
  },
}
