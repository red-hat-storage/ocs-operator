{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'ceph-blocklist-alerts.rules',
        rules: [
          {
            alert: 'ODFRBDClientBlocked',
            expr: |||
              ocs_rbd_client_blocklisted{%(ocsExporterSelector)s} > 0
            ||| % $._config,
            'for': $._config.blockedRBDClientAlertTime,
            labels: {
              severity: 'critical',
            },
            annotations: {
              message: 'A kernel RBD client on node {{ $labels.node_name }} is blocklisted by Ceph.',
              description: 'A kernel RBD client on node {{ $labels.node_name }} is blocklisted by Ceph for more than %s. Please check Ceph logs.' % $._config.blockedRBDClientAlertTime,
              severity_level: 'error',
            },
          },
        ],
      },
    ],
  },
}
