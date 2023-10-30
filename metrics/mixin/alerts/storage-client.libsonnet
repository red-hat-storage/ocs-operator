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
            # provider version should be same as client version or client can lag by one minor version, ie, client == provider || client == provider-1
            alert: 'StorageClientIncompatiblePlatformVersion',
            expr: |||
              floor((ocs_storage_client_cluster_version > 0) - ignoring(storage_consumer_name) group_left() (ocs_storage_provider_cluster_version > 0) / 1000) > %(clientPlatformCanLagByMinorVer)d <= -%(clientPlatformCanLagByMinorVer)d
            ||| % $._config,
            labels: {
              severity: 'warning',
            },
            annotations: {
              message: 'Storage Client Platform ({{ $labels.storage_consumer_name }}) lags by more than %d minor version' % $._config.clientPlatformCanLagByMinorVer,
              description: 'Storage Client Platform ({{ $labels.storage_consumer_name }}) lags by more than %d minor version. Client configuration may be incompatible' % $._config.clientPlatformCanLagByMinorVer,
              severity_level: 'warning',
            },
          },
          {
            # divide by 1000 here removes patch version
            # provider version should be same as client version or client can lag by one minor version, ie, client == provider || client == provider-1
            alert: 'StorageClientIncompatibleOperatorVersion',
            expr: |||
              floor((ocs_storage_client_operator_version > 0) - ignoring(storage_consumer_name) group_left() (ocs_storage_provider_operator_version > 0) / 1000) > %(clientOperatorCanLagByMinorVer)d <= -%(clientOperatorCanLagByMinorVer)d
            ||| % $._config,
            labels: {
              severity: 'warning',
            },
            annotations: {
              message: 'Storage Client Operator ({{ $labels.storage_consumer_name }}) lags by more than %d minor version' % $._config.clientOperatorCanLagByMinorVer,
              description: 'Storage Client Operator ({{ $labels.storage_consumer_name }}) lags by more than %d minor version. Client configuration may be incompatible' % $._config.clientOperatorCanLagByMinorVer,
              severity_level: 'warning',
            },
          },
        ],
      },
    ],
  }
}
