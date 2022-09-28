FROM golang:1.18 as builder

WORKDIR /workspace

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags '-extldflags "-static"' -o metrics-exporter metrics/main.go
RUN go version | tee /go.version

FROM quay.io/ceph/ceph:v17

ENV MEBIN=/usr/local/bin/metrics-exporter

COPY --from=builder workspace/metrics-exporter "$MEBIN"
COPY --from=builder /go.version /go.version

RUN chmod +x "$MEBIN"

ENTRYPOINT ["/usr/local/bin/metrics-exporter"]