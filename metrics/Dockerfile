# Build stage 1

FROM quay.io/ceph/ceph:v18 AS updated_base

# TODO: remove the following cmd, when issues
# https://github.com/ceph/ceph-container/issues/2034
# https://github.com/ceph/ceph-container/issues/2141 are fixed.
RUN dnf -y reinstall https://download.ceph.com/rpm-${CEPH_VERSION}/el9/noarch/ceph-release-1-1.el9.noarch.rpm \
    && ( dnf config-manager --disable tcmu-runner,tcmu-runner-source,tcmu-runner-noarch,ceph-iscsi,ganesha || true ) \
    && true

FROM updated_base AS builder

ARG GOROOT=/usr/local/go
ARG GOLANG_VERSION=1.24.6
ARG GO_ARCH

RUN ( test -n "${GO_ARCH}" && exit 0; echo -e "\n\nMissing GO_ARCH argument for building image\n\n"; exit 1 ) && \
    mkdir -p ${GOROOT} && \
    curl -fsSL https://dl.google.com/go/go${GOLANG_VERSION}.linux-${GO_ARCH}.tar.gz | \
    tar xzf - -C ${GOROOT} --strip-components=1

RUN dnf -y install --nodocs \
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

RUN go build -ldflags "$LDFLAGS" -tags netgo,osusergo -o metrics-exporter ./metrics/main.go

# Build stage 2

FROM updated_base

COPY --from=builder /workspace/metrics-exporter /usr/local/bin/metrics-exporter

# verify that all dynamically linked libraries are available
RUN [ $(ldd /usr/local/bin/metrics-exporter | grep -c '=> not found') = '0' ]

USER 2024

ENTRYPOINT ["/usr/local/bin/metrics-exporter"]
