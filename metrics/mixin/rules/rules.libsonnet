{
  prometheusRules+:: {
    groups+: [
      {
        name: 'ocs-telemeter.rules',
        rules: [
          {
            record: 'ocs:node_num_cpu:sum',
            expr: |||
              sum(
                max(
                  (
                    kube_pod_container_resource_requests_cpu_cores{namespace="openshift-storage", pod =~"(noobaa-core.*|noobaa-db.*|rook-ceph-(mon|osd|mgr|mds|rgw).*)"} * 0 +1 
                  ) * on(node) group_left() node:node_num_cpu:sum
                ) by (node)
              )
            ||| % $._config,
          },
          {
            record: 'ocs:container_cpu_usage:sum',
            expr: |||
              sum(
                rate(
                  container_cpu_usage_seconds_total{namespace="openshift-storage",container="",pod =~"(noobaa-core.*|noobaa-db.*|rook-ceph-(mon|osd|mgr|mds|rgw).*)"} [5m]
                )
              )
            ||| % $._config,
          },
        ],
      },
    ],
  },
}
