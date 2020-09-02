local kp = (import 'jsonnet/kube-prometheus-external.libsonnet') + {
  _config+:: {
    namespace: 'openshift-storage',
  },
};

{ ['prometheus-' + name + '-external']: kp.prometheus[name] for name in std.objectFields(kp.prometheus) }
