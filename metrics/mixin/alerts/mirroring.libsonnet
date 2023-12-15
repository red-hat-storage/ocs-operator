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
              message: 'Mirror daemon is unhealthy in namespace:cluster {{ $labels.namespace }}:{{ $labels.managedBy }}.',
              description: 'Mirror daemon is in unhealthy status for more than %s. Mirroring is not working as expected in namespace:cluster {{ $labels.namespace }}:{{ $labels.managedBy }}.' % $._config.odfMirrorDaemonStatusAlertTime,
              storage_type: $._config.cephStorageType,
              severity_level: 'error',
              runbook_url: 'https://github.com/openshift/runbooks/blob/master/alerts/openshift-container-storage-operator/OdfMirrorDaemonStatus.md',
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
              mirroring_image_status: 'unknown',
            },
            annotations: {
              message: 'Mirroring image(s) (PV) in the pool {{ $labels.name }} are in Unknown state in namespace:cluster {{ $labels.namespace }}:{{ $labels.managedBy }}.',
              description: 'Mirroring image(s) (PV) in the pool {{ $labels.name }} are in Unknown state for more than %s. Mirroring might not work as expected in namespace:cluster {{ $labels.namespace }}:{{ $labels.managedBy }}.' % $._config.odfPoolMirroringImageHealthWarningAlertTime,
              storage_type: $._config.cephStorageType,
              severity_level: 'warning',
              runbook_url: 'https://github.com/openshift/runbooks/blob/master/alerts/openshift-container-storage-operator/OdfPoolMirroringImageHealth.md',
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
              mirroring_image_status: 'warning',
            },
            annotations: {
              message: 'Mirroring image(s) (PV) in the pool {{ $labels.name }} are in Warning state for cluster {{ $labels.namespace }}:{{ $labels.managedBy }}.',
              description: 'Mirroring image(s) (PV) in the pool {{ $labels.name }} are in Warning state for more than %s. Mirroring might not work as expected in namespace:cluster {{ $labels.namespace }}:{{ $labels.managedBy }}.' % $._config.odfPoolMirroringImageHealthWarningAlertTime,
              storage_type: $._config.cephStorageType,
              severity_level: 'warning',
              runbook_url: 'https://github.com/openshift/runbooks/blob/master/alerts/openshift-container-storage-operator/OdfPoolMirroringImageHealth.md',
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
              mirroring_image_status: 'error',
            },
            annotations: {
              message: 'Mirroring image(s) (PV) in the pool {{ $labels.name }} are in Error state for cluster {{ $labels.namespace }}:{{ $labels.managedBy }}.',
              description: 'Mirroring image(s) (PV) in the pool {{ $labels.name }} are in Error state for more than %s. Mirroring is not working as expected in namespace:cluster {{ $labels.namespace }}:{{ $labels.managedBy }}.' % $._config.odfPoolMirroringImageHealthCriticalAlertTime,
              storage_type: $._config.cephStorageType,
              severity_level: 'error',
              runbook_url: 'https://github.com/openshift/runbooks/blob/master/alerts/openshift-container-storage-operator/OdfPoolMirroringImageHealth.md',
            },
          },
          {
            alert: 'ODFPersistentVolumeMirrorStatus',
            expr: |||
              ocs_rbd_mirror_image_state{%(ocsExporterSelector)s} * on(image,pool_name) group_left(name,namespace) ocs_rbd_pv_metadata{%(ocsExporterSelector)s} == 1
            ||| % $._config,
            'for': $._config.odfPoolMirroringImageHealthWarningAlertTime,
            labels: {
              severity: 'critical',
            },
            annotations: {
              message: 'Persistent volume {{ $labels.name }}/{{ $labels.namespace }} is not mirrored properly to peer site {{ $labels.site_name }} in namespace:cluster {{ $labels.namespace }}:{{ $labels.managedBy }}.',
              description: 'Persistent volume {{ $labels.name }}/{{ $labels.namespace }} is not mirrored properly to peer site {{ $labels.site_name }} for more than %s. RBD image={{ $labels.image }} and CephBlockPool={{ $labels.pool_name }}. Please check namespace:cluster {{ $labels.namespace }}:{{ $labels.managedBy }}.' % $._config.odfPoolMirroringImageHealthWarningAlertTime,
              storage_type: $._config.cephStorageType,
              severity_level: 'error',
              runbook_url: 'https://github.com/openshift/runbooks/blob/master/alerts/openshift-container-storage-operator/ODFPersistentVolumeMirrorStatus.md'
            },
          },
          {
            alert: 'ODFPersistentVolumeMirrorStatus',
            expr: |||
              ocs_rbd_mirror_image_state{%(ocsExporterSelector)s} * on(image,pool_name) group_left(name,namespace) ocs_rbd_pv_metadata{%(ocsExporterSelector)s} == 0
            ||| % $._config,
            'for': $._config.odfPoolMirroringImageHealthWarningAlertTime,
            labels: {
              severity: 'warning',
            },
            annotations: {
              message: 'Status unknown for Persistent volume {{ $labels.name }}/{{ $labels.namespace }} mirroring to peer site {{ $labels.site_name }} in namespace:cluster {{ $labels.namespace }}:{{ $labels.managedBy }}.',
              description: 'Status unknown for Persistent volume {{ $labels.name }}/{{ $labels.namespace }} to peer site {{ $labels.site_name }} for more than %s. RBD image={{ $labels.image }} and CephBlockPool={{ $labels.pool_name }}. Please check namespace:cluster {{ $labels.namespace }}:{{ $labels.managedBy }}' % $._config.odfPoolMirroringImageHealthWarningAlertTime,
              storage_type: $._config.cephStorageType,
              severity_level: 'warning',
              runbook_url: 'https://github.com/openshift/runbooks/blob/master/alerts/openshift-container-storage-operator/ODFPersistentVolumeMirrorStatus.md'
            },
          },
        ],
      },
    ],
  },

}
