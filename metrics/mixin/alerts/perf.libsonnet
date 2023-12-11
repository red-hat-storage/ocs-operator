{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'mds-performance-alerts.rules',
        rules: [
          {
            alert: 'MDSCacheUsageHigh',
            expr: |||
              ceph_mds_mem_rss / on(ceph_daemon) group_left(job)(label_replace(kube_pod_container_resource_requests{container="mds", resource="memory"}, "ceph_daemon", "mds.$1", "pod", "rook-ceph-mds-(.*)-(.*)") * .5) > .95
            ||| % $._config,
            'for': $._config.mdsCacheUsageAlertTime,
            labels: {
              severity: 'critical',
            },
            annotations: {
              message: 'High MDS cache usage for the daemon {{ $labels.ceph_daemon }}.',
              description: 'MDS cache usage for the daemon {{ $labels.ceph_daemon }} has exceeded above 95% of the requested value. Increase the memory request for {{ $labels.ceph_daemon }} pod.',
              severity_level: 'error',
            },
          },
        ],
      },
    ],
  },
}
