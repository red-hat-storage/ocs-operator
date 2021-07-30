FROM golang:latest AS builder

RUN GOOS=linux GOARCH=amd64 GO111MODULE=on go get github.com/go-delve/delve/cmd/dlv

# ==============================================

FROM registry.access.redhat.com/ubi8/ubi-minimal

COPY --from=builder /go/bin/dlv /

RUN chown root:root /dlv

ADD extras/config.yml /.config/dlv/
ADD extras/script.sh /script.sh
ADD build/_output/bin/ocs-operator /usr/local/bin/ocs-operator
ADD build/_output/bin/metrics-exporter /usr/local/bin/metrics-exporter
ADD build/_output/*rules*.yaml /ocs-prometheus-rules/

RUN chmod 755 /script.sh

EXPOSE 40000
