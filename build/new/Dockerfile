# Build manager binaries

FROM golang:1.18 as builder

WORKDIR /workspace

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags '-extldflags "-static"' -o ocs-operator main.go
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags '-extldflags "-static"' -o provider-api services/provider/main.go
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags '-extldflags "-static"' -o status-reporter services/consumer/main.go
RUN go version | tee /go.version

# Copy the binaries to the minimal image
FROM registry.access.redhat.com/ubi8/ubi-minimal

COPY --from=builder workspace/ocs-operator /usr/local/bin/ocs-operator
COPY --from=builder workspace/provider-api /usr/local/bin/provider-api
COPY --from=builder workspace/status-reporter /usr/local/bin/status-reporter
COPY --from=builder workspace/metrics/deploy/*rules*.yaml /ocs-prometheus-rules/
COPY --from=builder /go.version /go.version

RUN chmod +x /usr/local/bin/ocs-operator /usr/local/bin/provider-api /usr/local/bin/status-reporter

USER operator

ENTRYPOINT ["/usr/local/bin/ocs-operator"]
