{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'storage-cluster-alerts.rules',
        rules: [
          {
            alert: 'CephMonLowNumber',
            expr: |||
              ((count by (managedBy, namespace) (ceph_mon_metadata)) - on(managedBy,namespace) group_right() (ocs_storagecluster_failure_domain_count>=5)) < 0
            |||,
            labels: {
              severity: 'info',
            },
            annotations: {
              description: 'The number of node failure zones available (5) allow to increase the number of Ceph monitors from 3 to 5 in order to improve cluster resilience.',
              message: 'The current number of Ceph monitors can be increased in order to improve cluster resilience.',
              severity_level: 'info',
              storage_type: 'ceph',
              runbook_url: 'https://github.com/openshift/runbooks/blob/master/alerts/openshift-container-storage-operator/CephMonLowNumber.md',
            },
          },
        ],
      },
    ],
  }
}
