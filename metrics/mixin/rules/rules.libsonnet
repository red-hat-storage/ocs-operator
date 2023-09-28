{
  prometheusRules+:: {
    groups+: [
      {
        name: 'ocs_performance.rules',
        rules: [
          {
            record: 'cluster:ceph_disk_latency_read:join_ceph_node_disk_rate1m',
            expr: |||
              sum by (namespace, managedBy) (
                  topk by (ceph_daemon) (1, label_replace(label_replace(ceph_disk_occupation{job="rook-ceph-mgr"}, "instance", "$1", "exported_instance", "(.*)"), "device", "$1", "device", "/dev/(.*)")) 
                * on(instance, device) group_left topk by (instance,device) 
                  (1,
                    (
                      rate(node_disk_read_time_seconds_total[1m]) / (clamp_min(rate(node_disk_reads_completed_total[1m]), 1))
                    )
                  )
              )
            ||| % $._config,
          },
          {
            record: 'cluster:ceph_disk_latency_write:join_ceph_node_disk_rate1m',
            expr: |||
              sum by (namespace, managedBy) (
                  topk by (ceph_daemon) (1, label_replace(label_replace(ceph_disk_occupation{job="rook-ceph-mgr"}, "instance", "$1", "exported_instance", "(.*)"), "device", "$1", "device", "/dev/(.*)")) 
                * on(instance, device) group_left topk by (instance,device) 
                  (1,
                    (
                      rate(node_disk_write_time_seconds_total[1m]) / (clamp_min(rate(node_disk_writes_completed_total[1m]), 1))
                    )
                  )
              )
            ||| % $._config,
          },
        ],
      },
      {
        name: 'ODF_standardized_metrics.rules',
        rules: [
          {
            record: 'odf_system_health_status',
            expr: |||
              ceph_health_status
            ||| % $._config,
            labels: {
              system_vendor: 'Red Hat',
              system_type: 'OCS',
            },
          },
          {
            record: 'odf_system_raw_capacity_total_bytes',
            expr: |||
              ceph_cluster_total_bytes
            ||| % $._config,
            labels: {
              system_vendor: 'Red Hat',
              system_type: 'OCS',
            },
          },
          {
            record: 'odf_system_raw_capacity_used_bytes',
            expr: |||
              ceph_cluster_total_used_raw_bytes
            ||| % $._config,
            labels: {
              system_vendor: 'Red Hat',
              system_type: 'OCS',
            },
          },
          {
            record: 'odf_system_iops_total_bytes',
            expr: |||
              sum by (namespace, managedBy, job, service) (rate(ceph_pool_wr[1m]) + rate(ceph_pool_rd[1m]))
            ||| % $._config,
            labels: {
              system_vendor: 'Red Hat',
              system_type: 'OCS',
            },
          },
          {
            record: 'odf_system_throughput_total_bytes',
            expr: |||
              sum by (namespace, managedBy, job, service) (rate(ceph_pool_wr_bytes[1m]) + rate(ceph_pool_rd_bytes[1m]))
            ||| % $._config,
            labels: {
              system_vendor: 'Red Hat',
              system_type: 'OCS',
            },
          },
          {
            record: 'odf_system_latency_seconds',
            expr: |||
              sum by (namespace, managedBy, job, service)
              (
                topk by (ceph_daemon) (1, label_replace(label_replace(ceph_disk_occupation{job="rook-ceph-mgr"}, "instance", "$1", "exported_instance", "(.*)"), "device", "$1", "device", "/dev/(.*)")) 
                * on(instance, device) group_left() topk by (instance,device) 
                (1,
                  (
                    (  
                        rate(node_disk_read_time_seconds_total[1m]) / (clamp_min(rate(node_disk_reads_completed_total[1m]), 1))
                    ) +
                    (
                        rate(node_disk_write_time_seconds_total[1m]) / (clamp_min(rate(node_disk_writes_completed_total[1m]), 1))
                    )
                  )
                )
              )
            ||| % $._config,
            labels: {
              system_vendor: 'Red Hat',
              system_type: 'OCS',
            },
          },
          {
            record: 'odf_system_objects_total',
            expr: |||
              sum (ocs_objectbucket_objects_total)
            ||| % $._config,
            labels: {
              system_vendor: 'Red Hat',
              system_type: 'OCS',
            },
          },
          {
            record: 'odf_system_bucket_count',
            expr: |||
              sum (ocs_objectbucket_count_total)
            ||| % $._config,
            labels: {
              system_vendor: 'Red Hat',
              system_type: 'OCS',
            },
          },
        ],
      },
    ],
  },
}
