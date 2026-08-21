# Build stage 1

FROM docker.io/library/golang:1.26 AS builder

WORKDIR /workspace

# Cache module downloads in a separate layer.
COPY go.mod go.sum go.work ./
COPY api/go.mod api/go.sum ./api/
COPY metrics/go.mod metrics/go.sum ./metrics/
COPY services/provider/api/go.mod services/provider/api/go.sum ./services/provider/api/

RUN --mount=type=cache,target=/go/pkg/mod \
    go work sync && \
    go mod download

COPY . .

ARG LDFLAGS

RUN --mount=type=cache,target=/go/pkg/mod \
    go build -ldflags "$LDFLAGS" -tags netgo,osusergo -o ocs-operator cmd/main.go && \
    go build -ldflags "$LDFLAGS" -tags netgo,osusergo -o provider-api services/provider/main.go && \
    go build -tags netgo,osusergo -o onboarding-validation-keys-gen onboarding-validation-keys-generator/main.go

# Build stage 2

FROM registry.access.redhat.com/ubi9/ubi-minimal

COPY --from=builder workspace/ocs-operator /usr/local/bin/ocs-operator
COPY --from=builder workspace/provider-api /usr/local/bin/provider-api
COPY --from=builder workspace/onboarding-validation-keys-gen /usr/local/bin/onboarding-validation-keys-gen
COPY --from=builder workspace/metrics/deploy/*rules*.yaml /ocs-prometheus-rules/

RUN chmod +x /usr/local/bin/ocs-operator /usr/local/bin/provider-api

USER operator

ENTRYPOINT ["/usr/local/bin/ocs-operator"]
