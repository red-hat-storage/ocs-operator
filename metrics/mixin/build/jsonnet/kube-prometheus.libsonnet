local k = import '../vendor/ksonnet/ksonnet.beta.3/k.libsonnet';

(import 'prometheus.libsonnet') +
(import '../../mixin.libsonnet') + {
  kubePrometheus+:: {
    namespace: k.core.v1.namespace.new($._config.namespace),
    name: 'prometheus-ocs-rules',
  },
} + {
  _config+:: {
    namespace: 'default',

    prometheus+:: {
      rules: $.prometheusRules + $.prometheusAlerts,
    },
  },
}
