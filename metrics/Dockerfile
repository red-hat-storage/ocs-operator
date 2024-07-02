# Build stage 1

FROM docker.io/library/golang:1.22 as builder

WORKDIR /workspace

COPY . .

ARG GOOS=linux
ARG GOARCH=amd64
ARG LDFLAGS

RUN GOOS="$GOOS" GOARCH="$GOARCH" go build -ldflags "$LDFLAGS" -tags netgo,osusergo -o metrics-exporter main.go

# Build stage 2

FROM quay.io/ceph/ceph:v18

COPY --from=builder workspace/metrics-exporter /usr/local/bin/metrics-exporter

RUN chmod +x /usr/local/bin/metrics-exporter

USER 2024

ENTRYPOINT ["/usr/local/bin/metrics-exporter"]
