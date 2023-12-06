{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'ceph-daemon-performance-alerts.rules',
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
            }
          },
          {
            alert: 'OSDCPULoadHigh',
            expr: |||
              pod:container_cpu_usage:sum{%(osdSelector)s} > 0.35 
            ||| % $._config,
            'for': $._config.osdCPULoadHighAlertTime,
            labels: {
              severity: 'warning',
            },
            annotations: {
              message: 'High CPU usage detected in OSD container on pod {{ $labels.pod}}.',
              description: 'High CPU usage in the OSD container on pod {{ $labels.pod }}. Please create more OSDs to increase performance',
              severity_level: 'warning',
            },
          },
        ],
      },
    ],
  },
}
