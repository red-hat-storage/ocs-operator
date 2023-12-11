{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'external-ocs-encryption-alert.rules',
        rules: [
          {
            alert: 'KMSServerConnectionAlert',
            expr: |||
              ocs_storagecluster_kms_connection_status{%(ocsExporterSelector)s} == 1
            ||| % $._config,
            'for': $._config.ocsStorageClusterKMSConnectionAlert,
            labels: {
              severity: 'critical',
            },
            annotations: {
              message: 'Storage Cluster KMS Server is in un-connected state. Please check KMS config in namespace:cluster {{ $labels.namespace }}:{{ $labels.managedBy }}.',
              description: 'Storage Cluster KMS Server is in un-connected state for more than %s. Please check KMS config in namespace:cluster {{ $labels.namespace }}:{{ $labels.managedBy }}.' % $._config.ocsStorageClusterKMSConnectionAlert,
              storage_type: $._config.cephStorageType,
              severity_level: 'error',
            },
          },
        ],
      },
    ],
  },
}
