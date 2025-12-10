# Build stage 1

FROM docker.io/library/golang:1.24 AS builder

WORKDIR /workspace

COPY . .

ARG LDFLAGS

RUN go build -ldflags "$LDFLAGS" -tags netgo,osusergo -o metrics-exporter ./metrics/main.go

# Build stage 2

FROM quay.io/ceph/ceph:v18

COPY --from=builder workspace/metrics-exporter /usr/local/bin/metrics-exporter

RUN chmod +x /usr/local/bin/metrics-exporter

USER 2024

ENTRYPOINT ["/usr/local/bin/metrics-exporter"]
