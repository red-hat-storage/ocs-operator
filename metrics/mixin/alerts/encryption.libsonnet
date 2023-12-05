{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'ocs-encryption-alert.rules',
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
              message: 'Storage Cluster KMS Server is in un-connected state. Please check KMS config.',
              description: 'Storage Cluster KMS Server is in un-connected state for more than %s. Please check KMS config.' % $._config.ocsStorageClusterKMSConnectionAlert,
              storage_type: $._config.cephStorageType,
              severity_level: 'error',
              runbook_url: 'https://github.com/openshift/runbooks/blob/master/alerts/openshift-container-storage-operator/KMSServerConnectionAlert.md',
            },
          },
        ],
      },
    ],
  },
}
