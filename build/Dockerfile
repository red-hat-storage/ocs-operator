FROM registry.access.redhat.com/ubi8/ubi-minimal

USER nobody

ADD _output/bin/ocs-operator /usr/local/bin/ocs-operator
ADD _output/bin/provider-api /usr/local/bin/provider-api
ADD _output/bin/status-reporter /usr/local/bin/status-reporter
ADD _output/*rules*.yaml /ocs-prometheus-rules/
