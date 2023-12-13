{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'external-cluster-services-alert.rules',
        rules: [
          {
            alert: 'ClusterObjectStoreState',
            expr: |||
              ocs_rgw_health_status{%(ocsExporterSelector)s} == 2
            ||| % $._config,
            'for': $._config.clusterObjectStoreStateAlertTime,
            labels: {
              severity: 'critical',
            },
            annotations: {
              message: 'Cluster Object Store is in unhealthy state. Please check Ceph cluster health or RGW connection in namespace:cluster {{ $labels.namespace }}:{{ $labels.managedBy }}.',
              runbook_url: 'https://github.com/openshift/runbooks/blob/master/alerts/openshift-container-storage-operator/ClusterObjectStoreState.md',
              description: 'Cluster Object Store is in unhealthy state for more than %s. Please check Ceph cluster health or RGW connection in namespace:cluster {{ $labels.namespace }}:{{ $labels.managedBy }}.' % $._config.clusterObjectStoreStateAlertTime,
              storage_type: $._config.objectStorageType,
              severity_level: 'error',
            },
          },
        ],
      },
    ],
  },
}
