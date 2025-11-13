# Build stage
FROM docker.io/library/golang:1.24 AS builder

WORKDIR /workspace

# Copy source code
COPY . .

# Build devicefinder binary
ARG LDFLAGS
RUN go build -ldflags "$LDFLAGS" -tags netgo,osusergo -o devicefinder services/devicefinder/cmd/devicefinder/main.go services/devicefinder/cmd/devicefinder/discovery.go

# Runtime stage
FROM registry.access.redhat.com/ubi9/ubi-minimal

# Install udev package
RUN microdnf install -y udev && \
    microdnf clean all

COPY --from=builder workspace/devicefinder /usr/local/bin/devicefinder

RUN chmod +x /usr/local/bin/devicefinder

USER 1001

ENTRYPOINT ["/usr/local/bin/devicefinder"]
