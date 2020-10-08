{
  _config+:: {
    // Selectors are inserted between {} in Prometheus queries.
    ocsExporterSelector: 'job="ocs-metrics-exporter"',

    // Duration to raise various Alerts
    clusterObjectStoreStateAlertTime: '15s',

    // Constants
    objectStorageType: 'RGW',

    // We build alerts for the presence of all these jobs.
    jobs: {
      ocsExporter: $._config.ExporterSelector,
    },
  },
}
