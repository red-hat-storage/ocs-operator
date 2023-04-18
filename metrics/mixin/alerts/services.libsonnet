{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'cluster-services-alert.rules',
        rules: [
          {
            alert: 'ClusterObjectStoreState',
            expr: |||
              ocs_rgw_health_status{%(ocsExporterSelector)s} > 1
            ||| % $._config,
            'for': $._config.clusterObjectStoreStateAlertTime,
            labels: {
              severity: 'critical',
            },
            annotations: {
              message: 'Cluster Object Store is in unhealthy state. Please check Ceph cluster health.',
              description: 'RGW endpoint of the Ceph object store is in a failure state for more than %s. Please check the health of the Ceph cluster.' % $._config.clusterObjectStoreStateAlertTime,
              storage_type: $._config.objectStorageType,
              severity_level: 'error',
            },
          },
        ],
      },
    ],
  },
}
