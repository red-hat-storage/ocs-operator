# Build stage 1

FROM docker.io/library/golang:1.22 as builder

WORKDIR /workspace

COPY . .

ARG GOOS=linux
ARG GOARCH=amd64
ARG LDFLAGS

RUN GOOS="$GOOS" GOARCH="$GOARCH" go build -ldflags "$LDFLAGS" -tags netgo,osusergo -o ocs-operator main.go
RUN GOOS="$GOOS" GOARCH="$GOARCH" go build -ldflags "$LDFLAGS" -tags netgo,osusergo -o provider-api services/provider/main.go
RUN GOOS="$GOOS" GOARCH="$GOARCH" go build -tags netgo,osusergo -o onboarding-validation-keys-gen onboarding-validation-keys-generator/main.go
RUN GOOS="$GOOS" GOARCH="$GOARCH" go build -tags netgo,osusergo -o ux-backend-server services/ux-backend/main.go

# Build stage 2

FROM registry.access.redhat.com/ubi9/ubi-minimal

COPY --from=builder workspace/ocs-operator /usr/local/bin/ocs-operator
COPY --from=builder workspace/provider-api /usr/local/bin/provider-api
COPY --from=builder workspace/onboarding-validation-keys-gen /usr/local/bin/onboarding-validation-keys-gen
COPY --from=builder workspace/metrics/deploy/*rules*.yaml /ocs-prometheus-rules/
COPY --from=builder workspace/ux-backend-server /usr/local/bin/ux-backend-server
COPY --from=builder workspace/hack/crdavail.sh /usr/local/bin/crdavail

RUN chmod +x /usr/local/bin/ocs-operator /usr/local/bin/provider-api /usr/local/bin/crdavail

USER operator

ENTRYPOINT ["/usr/local/bin/crdavail"]
