local kp = (import 'jsonnet/kube-prometheus.libsonnet') + {
  _config+:: {
    namespace: 'openshift-storage',
  },
};

{ ['prometheus-' + name]: kp.prometheus[name] for name in std.objectFields(kp.prometheus) }
