# https://gitlab.cee.redhat.com/ceph/rhodf/-/blob/rhodf-4.99-rhel-8/distgit/containers/ocs-operator/Dockerfile.in

FROM golang:1.18 as builder

WORKDIR /workspace

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags '-extldflags "-static"' -o ocs-operator main.go
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags '-extldflags "-static"' -o provider-api services/provider/main.go
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags '-extldflags "-static"' -o status-reporter services/consumer/main.go
RUN go version | tee /go.version

FROM registry.access.redhat.com/ubi8/ubi-minimal

ENV OPBIN=/usr/local/bin/ocs-operator
ENV PRBIN=/usr/local/bin/provider-api
ENV STATBIN=/usr/local/bin/status-reporter
ENV PROMRULES=/ocs-prometheus-rules

COPY --from=builder workspace/ocs-operator "$OPBIN"
COPY --from=builder workspace/provider-api "$PRBIN"
COPY --from=builder workspace/status-reporter "$STATBIN"
COPY --from=builder workspace/metrics/deploy/*rules*.yaml "$PROMRULES"
COPY --from=builder /go.version /go.version

RUN chmod +x "$OPBIN" "$PRBIN" "$STATBIN"

USER operator

ENTRYPOINT ["/usr/local/bin/ocs-operator"]