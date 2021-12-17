{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'mirroring-alert.rules',
        rules: [
          {
            alert: 'OdfMirrorDaemonStatus',
            expr: |||
              ((count by(namespace) (ocs_mirror_daemon_count{%(ocsExporterSelector)s} == 0)) * on(namespace) group_left() (count by(namespace) (ocs_pool_mirroring_status{%(ocsExporterSelector)s} == 1))) > 0
            ||| % $._config,
            'for': $._config.odfMirrorDaemonStatusAlertTime,
            labels: {
              severity: 'critical',
            },
            annotations: {
              message: 'Mirror daemon is unhealthy.',
              description: 'Mirror daemon is in unhealthy status for more than %s. Mirroring on this cluster is not working as expected.' % $._config.odfMirrorDaemonStatusAlertTime,
              storage_type: $._config.cephStorageType,
              severity_level: 'error',
            },
          },
          {
            alert: 'OdfPoolMirroringImageHealth',
            expr: |||
              (ocs_pool_mirroring_image_health{%(ocsExporterSelector)s}  * on (namespace) group_left() (max by(namespace) (ocs_pool_mirroring_status{%(ocsExporterSelector)s}))) == 1
            ||| % $._config,
            'for': $._config.odfPoolMirroringImageHealthWarningAlertTime,
            labels: {
              severity: 'warning',
            },
            annotations: {
              message: 'Mirroring image(s) (PV) in the pool {{ $labels.name }} are in Unknown state.',
              description: 'Mirroring image(s) (PV) in the pool {{ $labels.name }} are in Unknown state for more than %s. Mirroring might not work as expected.' % $._config.odfPoolMirroringImageHealthWarningAlertTime,
              storage_type: $._config.cephStorageType,
              severity_level: 'warning',
            },
          },
          {
            alert: 'OdfPoolMirroringImageHealth',
            expr: |||
              (ocs_pool_mirroring_image_health{%(ocsExporterSelector)s}  * on (namespace) group_left() (max by(namespace) (ocs_pool_mirroring_status{%(ocsExporterSelector)s}))) == 2
            ||| % $._config,
            'for': $._config.odfPoolMirroringImageHealthWarningAlertTime,
            labels: {
              severity: 'warning',
            },
            annotations: {
              message: 'Mirroring image(s) (PV) in the pool {{ $labels.name }} are in Warning state.',
              description: 'Mirroring image(s) (PV) in the pool {{ $labels.name }} are in Warning state for more than %s. Mirroring might not work as expected.' % $._config.odfPoolMirroringImageHealthWarningAlertTime,
              storage_type: $._config.cephStorageType,
              severity_level: 'warning',
            },
          },
          {
            alert: 'OdfPoolMirroringImageHealth',
            expr: |||
              (ocs_pool_mirroring_image_health{%(ocsExporterSelector)s}  * on (namespace) group_left() (max by(namespace) (ocs_pool_mirroring_status{%(ocsExporterSelector)s}))) == 3
            ||| % $._config,
            'for': $._config.odfPoolMirroringImageHealthCriticalAlertTime,
            labels: {
              severity: 'critical',
            },
            annotations: {
              message: 'Mirroring image(s) (PV) in the pool {{ $labels.name }} are in Error state.',
              description: 'Mirroring image(s) (PV) in the pool {{ $labels.name }} are in Error state for more than %s. Mirroring is not working as expected.' % $._config.odfPoolMirroringImageHealthCriticalAlertTime,
              storage_type: $._config.cephStorageType,
              severity_level: 'error',
            },
          },
        ],
      },
    ],
  },

}
