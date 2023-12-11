# Build stage 1

FROM golang:1.20 as builder

WORKDIR /workspace

COPY . .

ARG LDFLAGS

RUN GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -tags netgo,osusergo -o ocs-operator main.go
RUN GOOS=linux GOARCH=amd64 go build -tags netgo,osusergo -o provider-api services/provider/main.go
RUN GOOS=linux GOARCH=amd64 go build -tags netgo,osusergo -o onboarding-secret-generator onboarding/main.go

# Build stage 2

FROM registry.access.redhat.com/ubi9/ubi-minimal

COPY --from=builder workspace/ocs-operator /usr/local/bin/ocs-operator
COPY --from=builder workspace/provider-api /usr/local/bin/provider-api
COPY --from=builder workspace/onboarding-secret-generator /usr/local/bin/onboarding-secret-generator
COPY --from=builder workspace/metrics/deploy/*rules*.yaml /ocs-prometheus-rules/

RUN chmod +x /usr/local/bin/ocs-operator /usr/local/bin/provider-api

USER operator

ENTRYPOINT ["/usr/local/bin/ocs-operator"]
