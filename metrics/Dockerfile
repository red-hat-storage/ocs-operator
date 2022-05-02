FROM golang:1.17 as builder

WORKDIR /go/src/github.com/red-hat-storage/ocs-operator
COPY . .
USER root
RUN make build-go

FROM quay.io/ceph/ceph:v17

COPY --from=builder /go/src/github.com/red-hat-storage/ocs-operator/build/_output/bin/metrics-exporter /usr/local/bin/metrics-exporter

ENTRYPOINT ["/metrics-exporter"]

EXPOSE 8080 8081
