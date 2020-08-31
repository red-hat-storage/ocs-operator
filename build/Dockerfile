FROM registry.access.redhat.com/ubi8/ubi-minimal

USER nobody

ADD _output/bin/ocs-operator /usr/local/bin/ocs-operator
ADD _output/bin/metrics-exporter /usr/local/bin/metrics-exporter
ADD _output/*rules*.yaml /ocs-prometheus-rules/