{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'ceph-daemon-performance-alerts.rules',
        rules: [
          {
            alert: 'MDSCacheUsageHigh',
            expr: |||
              (ceph_mds_mem_rss * 1000) / on(ceph_daemon) group_left(job)(label_replace(kube_pod_container_resource_requests{container="mds", resource="memory"}, "ceph_daemon", "mds.$1", "pod", "rook-ceph-mds-(.*)-(.*)") * .5) > .95
            ||| % $._config,
            'for': $._config.mdsCacheUsageAlertTime,
            labels: {
              severity: 'critical',
            },
            annotations: {
              message: 'High MDS cache usage for the daemon {{ $labels.ceph_daemon }}.',
              description: 'MDS cache usage for the daemon {{ $labels.ceph_daemon }} has exceeded above 95% of the requested value. Increase the memory request for {{ $labels.ceph_daemon }} pod.',
              severity_level: 'error',
              runbook_url: 'https://github.com/openshift/runbooks/blob/master/alerts/openshift-container-storage-operator/CephMdsCacheUsageHigh.md',
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

          {
            alert: 'MDSCPUUsageHigh',
            expr: |||
              pod:container_cpu_usage:sum{%(mdsSelector)s}/ on(pod) kube_pod_resource_limit{resource='cpu',%(mdsSelector)s} > 0.67
            ||| % $._config,
            'for': $._config.mds_cpu_usage_high_threshold_duration,
            labels: {
              severity: 'warning',
            },
            annotations: {
              message: 'Ceph metadata server pod ({{ $labels.pod }}) has high cpu usage',
              description: 'Ceph metadata server pod ({{ $labels.pod }}) has high cpu usage.\nPlease consider increasing the number of active metadata servers,\nit can be done by increasing the number of activeMetadataServers parameter in the StorageCluster CR.',
              severity_level: 'warning',
            },
          },
        ],
      },
    ],
  },
}
