# Build stage 1

FROM golang:1.20 as builder

WORKDIR /workspace

COPY . .

ARG LDFLAGS

RUN GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -tags netgo,osusergo -o metrics-exporter metrics/main.go

# Build stage 2

FROM quay.io/ceph/ceph:v17

COPY --from=builder workspace/metrics-exporter /usr/local/bin/metrics-exporter

RUN chmod +x /usr/local/bin/metrics-exporter

USER operator

ENTRYPOINT ["/usr/local/bin/metrics-exporter"]
