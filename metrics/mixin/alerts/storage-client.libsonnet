{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'storage-client-alerts.rules',
        rules: [
          {
            alert: 'StorageClientHeartbeatMissed',
            expr: |||
              (time() - %(clientCheckinWarnSec)d) > (ocs_storage_client_last_heartbeat > 0)
            ||| % $._config,
            labels: {
              severity: 'warning',
            },
            annotations: {
              message: 'Storage Client ({{ $labels.storage_consumer_name }}) heartbeat missed for more than %d (s)' % $._config.clientCheckinWarnSec,
              description: 'Storage Client ({{ $labels.storage_consumer_name }}) heartbeat missed for more than %d (s). Lossy network connectivity might exist' % $._config.clientCheckinWarnSec,
              severity_level: 'warning',
            },
          },
          {
            alert: 'StorageClientHeartbeatMissed',
            expr: |||
              (time() - %(clientCheckinCritSec)d) > (ocs_storage_client_last_heartbeat > 0)
            ||| % $._config,
            labels: {
              severity: 'critical',
            },
            annotations: {
              message: 'Storage Client ({{ $labels.storage_consumer_name }}) heartbeat missed for more than %d (s)' % $._config.clientCheckinCritSec,
              description: 'Storage Client ({{ $labels.storage_consumer_name }}) heartbeat missed for more than %d (s). Client might have lost internet connectivity' % $._config.clientCheckinCritSec,
              severity_level: 'critical',
            },
          },
          {
            # divide by 1000 here removes patch version
            # warn if client lags provider by one minor version
            alert: 'StorageClientIncompatibleOperatorVersion',
            expr: |||
              floor((ocs_storage_provider_operator_version>0)/1000) - ignoring(storage_consumer_name) group_right() floor((ocs_storage_client_operator_version>0)/1000) == %(clientOperatorMinorVerDiff)d
            ||| % $._config,
            labels: {
              severity: 'warning',
            },
            annotations: {
              message: 'Storage Client Operator ({{ $labels.storage_consumer_name }}) lags by %d minor version' % $._config.clientOperatorMinorVerDiff,
              description: 'Storage Client Operator ({{ $labels.storage_consumer_name }}) lags by %d minor version. Client configuration may be incompatible' % $._config.clientOperatorMinorVerDiff,
              severity_level: 'warning',
            },
          },
        ],
      },
    ],
  }
}
