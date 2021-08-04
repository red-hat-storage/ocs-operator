{
  prometheusRules+:: {
    groups+: [
      {
        name: 'ocs_performance.rules',
        rules: [
          {
            record: 'record: cluster:ceph_disk_latency_read:join_ceph_node_disk_rate1m',
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
            record: 'record: cluster:ceph_disk_latency_write:join_ceph_node_disk_rate1m',
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
    ],
  },
}
