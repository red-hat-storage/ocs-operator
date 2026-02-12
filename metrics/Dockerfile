# Build stage

FROM quay.io/ceph/ceph:v18 AS builder

ARG GO_ARCH=amd64
ARG GOROOT=/usr/local/go
ARG GOLANG_VERSION=1.24.6
ARG CEPH_VERSION=18.2.7

RUN mkdir -p ${GOROOT} && \
    curl -fsSL https://dl.google.com/go/go${GOLANG_VERSION}.linux-${GO_ARCH}.tar.gz | \
    tar xzf - -C ${GOROOT} --strip-components=1

RUN dnf -y reinstall https://download.ceph.com/rpm-${CEPH_VERSION}/el9/noarch/ceph-release-1-1.el9.noarch.rpm || true && \
    ( dnf config-manager --disable tcmu-runner,tcmu-runner-source,tcmu-runner-noarch,ceph-iscsi,ganesha || true ) && \
    dnf -y install --nodocs \
    librados-devel librbd-devel \
    /usr/bin/cc \
    && dnf clean all \
    && rm -rf /var/cache/yum

ENV GOROOT=${GOROOT} \
    GOPATH=/go \
    CGO_ENABLED=1 \
    PATH="${GOROOT}/bin:${GOPATH}/bin:${PATH}"

WORKDIR /workspace

COPY . .

ARG LDFLAGS

RUN go build -ldflags "$LDFLAGS" -o metrics-exporter ./metrics/main.go

# Final stage

FROM quay.io/ceph/ceph:v18

COPY --from=builder /workspace/metrics-exporter /usr/local/bin/metrics-exporter

RUN chmod +x /usr/local/bin/metrics-exporter

USER 2024

ENTRYPOINT ["/usr/local/bin/metrics-exporter"]
