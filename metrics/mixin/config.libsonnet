{
  _config+:: {
    // Selectors are inserted between {} in Prometheus queries.
    ocsExporterSelector: 'job="ocs-metrics-exporter"',
    osdSelector: 'pod=~"rook-ceph-osd-.*"',

    // Duration to raise various Alerts
    clusterObjectStoreStateAlertTime: '15s',
    odfClusterResourceQuotaAlertTime: '0s',
    odfMirrorDaemonStatusAlertTime: '1m',
    odfObcQuotaAlertTime: '10s',
    odfObcQuotaCriticalAlertTime: '0s',
    odfPoolMirroringImageHealthWarningAlertTime: '1m',
    odfPoolMirroringImageHealthCriticalAlertTime: '10s',
    blockedRBDClientAlertTime: '10s',
    ocsStorageClusterKMSConnectionAlert: '5s',
    mdsCacheUsageAlertTime: '5m',
    osdCPULoadHighAlertTime: '15m',

    // Constants
    objectStorageType: 'RGW',
    cephStorageType: 'ceph',

    // We build alerts for the presence of all these jobs.
    jobs: {
      ocsExporter: $._config.ExporterSelector,
    },

    // Storage Consumer
    clientCheckinWarnSec: 120,
    clientCheckinCritSec: 300,
    clientOperatorMinorVerDiff: 1,
  },
}
