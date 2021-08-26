{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'odf-overprovision-alert.rules',
        rules: [
          {
            alert: 'OdfClusterResourceQuotaNearLimit',
            expr: |||
              (ocs_clusterresourcequota_used/ocs_clusterresourcequota_hard) > 0.80
            ||| % $._config,
            'for': $._config.odfClusterResourceQuotaAlertTime,
            labels: {
              severity: 'warning',
            },
            annotations: {
              message: 'ClusterResourceQuota {{$labels.name}} used more than 80%.',
              description: 'ClusterResourceQuota used more than 80%. PVC provisioning via ODF StorageClass {{$labels.storageclass}} will be blocked for any request which would take the usage beyond the hard limit. Please check the current configuration in ClusterResourceQuota Custom Resource {{$labels.name}}.',
              storage_type: $._config.cephStorageType,
              severity_level: 'warning',
            },
          },
        ],
      },
    ],
  },
}
