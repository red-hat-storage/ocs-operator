# Build stage 1

FROM golang:1.21 as builder

RUN apt-get update && \
    apt-get -y install librados-dev librbd-dev

WORKDIR /workspace

ARG GOOS=linux
ARG GOARCH=amd64
ARG LDFLAGS

# Copy the Go Modules manifests
COPY go.mod go.sum ./
# cache deps before building and copying source so that we don't need to re-build as much
# and so that source changes don't invalidate our built layer
COPY vendor/ vendor/

# Copy the project source
COPY main.go Makefile ./
COPY hack/ hack/
COPY version/ version/
COPY templates/ templates/
COPY api/ api/
COPY controllers/ controllers/
COPY config/ config/
COPY pkg/ pkg/
COPY services/ services/

COPY tools/ tools/

# RUN GOOS="$GOOS" GOARCH="$GOARCH" go test -ldflags "$LDFLAGS" -v -cover ./tools/volume-migration/
RUN GOOS="$GOOS" GOARCH="$GOARCH" go build -ldflags "$LDFLAGS" -tags netgo,osusergo -o volume-migration ./tools/volume-migration/

RUN chmod +x volume-migration

# Build stage 2

FROM quay.io/ocs-dev/rook-ceph:latest-downstream as toolbox

FROM fedora

RUN dnf update -y && \
    dnf install --setopt=tsflags=nodocs -y librados2 librbd1 && \
    dnf clean all && \
    rm -rf /var/cache/yum

COPY --from=toolbox /usr/local/bin/toolbox.sh /usr/local/bin/
COPY --from=builder workspace/volume-migration /usr/local/bin/

ENTRYPOINT ["/usr/local/bin/volume-migration"]
