# Build stage 1

FROM quay.io/ceph/ceph:v18 AS updated_base

# TODO: remove the following cmd, when issues
# https://github.com/ceph/ceph-container/issues/2034
# https://github.com/ceph/ceph-container/issues/2141 are fixed.
RUN dnf -y reinstall https://download.ceph.com/rpm-${CEPH_VERSION}/el9/noarch/ceph-release-1-1.el9.noarch.rpm \
    && ( dnf config-manager --disable tcmu-runner,tcmu-runner-source,tcmu-runner-noarch,ceph-iscsi,ganesha || true ) \
    && true

# TODO: remove this once the ceph base image includes urllib3>=2.6.3 and requests>=2.32.0
# CVE-2025-66471: urllib3 Streaming API improperly handles highly compressed data
# CVE-2026-21441: Decompression-bomb safeguard bypass when following HTTP redirects
# Both vulnerabilities allow decompression bombs causing memory exhaustion and DoS
# Also upgrade requests to a version compatible with urllib3 2.x
RUN dnf -y install --nodocs python3-pip \
    && pip3 install --upgrade "urllib3>=2.6.3" "requests>=2.32.0" \
    && dnf -y remove python3-pip \
    && dnf clean all \
    && rm -rf /var/cache/yum

FROM updated_base AS builder

ARG GOROOT=/usr/local/go
ARG GOLANG_VERSION=1.26.0
ARG GO_ARCH

RUN if [ -z "${GO_ARCH}" ]; then \
        case "$(uname -m)" in \
            x86_64)  GO_ARCH=amd64 ;; \
            aarch64) GO_ARCH=arm64 ;; \
            *)       echo "unsupported arch: $(uname -m)"; exit 1 ;; \
        esac; \
    fi && \
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
