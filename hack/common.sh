#!/usr/bin/env bash

# shellcheck disable=SC2034
# disable unused variable warnings

GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"
GOHOSTOS="$(go env GOHOSTOS)"
GOHOSTARCH="$(go env GOHOSTARCH)"

# Current DEV version of the CSV
CSV_VERSION="${CSV_VERSION:-4.14.0}"
LDFLAGS="-X github.com/red-hat-storage/ocs-operator/v4/version.Version=${CSV_VERSION}"

# Tools & binaries versions and locations
LOCALBIN="$(pwd)/bin"
GINKGO="${LOCALBIN}/ginkgo"
GOLANGCI_LINT_VERSION="v1.51.1"
GOLANGCI_LINT="${LOCALBIN}/golangci-lint"
SHELLCHECK_VERSION="v0.9.0"
SHELLCHECK="${LOCALBIN}/shellcheck"

OUTDIR_TEMPLATES="deploy/csv-templates"
OUTDIR_CRDS="$OUTDIR_TEMPLATES/crds"

DEPLOY_YAML_PATH="deploy/deploy-with-olm.yaml"
PROMETHEUS_RULES_PATH="metrics/deploy"

GINKGO_TEST_SUITE="${GINKGO_TEST_SUITE:-ocs}"

# This env var allows developers to point to a custom oc tool that isn't in $PATH
# defaults to just using the 'oc' binary provided in $PATH
OCS_OC_PATH="${OCS_OC_PATH:-oc}"
OCS_FINAL_DIR="deploy/ocs-operator/manifests"
BUNDLEMANIFESTS_DIR="rbac"

NOOBAA_CSV="$OUTDIR_TEMPLATES/noobaa-csv.yaml"
ROOK_CSV="$OUTDIR_TEMPLATES/rook-csv.yaml.in"
OCS_CSV="$OUTDIR_TEMPLATES/ocs-operator.csv.yaml.in"

LATEST_ROOK_IMAGE="docker.io/rook/ceph:v1.12.2"
LATEST_NOOBAA_IMAGE="quay.io/noobaa/noobaa-operator:master-20230718"
LATEST_NOOBAA_CORE_IMAGE="quay.io/noobaa/noobaa-core:master-20230718"
LATEST_NOOBAA_DB_IMAGE="docker.io/centos/postgresql-12-centos8"
LATEST_CEPH_IMAGE="quay.io/ceph/ceph:v17"
LATEST_ROOK_CSIADDONS_IMAGE="quay.io/csiaddons/k8s-sidecar:v0.6.0"
# TODO: change image once the quay repo is changed
LATEST_MUST_GATHER_IMAGE="quay.io/ocs-dev/ocs-must-gather:latest"

IMAGE_REGISTRY="quay.io"
REGISTRY_NAMESPACE="ocs-dev"
IMAGE_TAG="latest"
OPERATOR_IMAGE_NAME="ocs-operator"
OPERATOR_BUNDLE_NAME="ocs-operator-bundle"
FILE_BASED_CATALOG_NAME="ocs-operator-catalog"
METRICS_EXPORTER_IMAGE_NAME="ocs-metrics-exporter"

OPERATOR_FULL_IMAGE_NAME="${IMAGE_REGISTRY}/${REGISTRY_NAMESPACE}/${OPERATOR_IMAGE_NAME}:${IMAGE_TAG}"
BUNDLE_FULL_IMAGE_NAME="${IMAGE_REGISTRY}/${REGISTRY_NAMESPACE}/${OPERATOR_BUNDLE_NAME}:${IMAGE_TAG}"
FILE_BASED_CATALOG_FULL_IMAGE_NAME="${IMAGE_REGISTRY}/${REGISTRY_NAMESPACE}/${FILE_BASED_CATALOG_NAME}:${IMAGE_TAG}"
METRICS_EXPORTER_FULL_IMAGE_NAME="${IMAGE_REGISTRY}/${REGISTRY_NAMESPACE}/${METRICS_EXPORTER_IMAGE_NAME}:${IMAGE_TAG}"

NOOBAA_BUNDLE_FULL_IMAGE_NAME="quay.io/noobaa/noobaa-operator-bundle:master-20230718"

OCS_OPERATOR_INSTALL="${OCS_OPERATOR_INSTALL:-false}"
OCS_CLUSTER_UNINSTALL="${OCS_CLUSTER_UNINSTALL:-false}"
OCS_SUBSCRIPTION_CHANNEL=${OCS_SUBSCRIPTION_CHANNEL:-alpha}
INSTALL_NAMESPACE="${INSTALL_NAMESPACE:-openshift-storage}"
UPGRADE_FROM_OCS_REGISTRY_IMAGE="${UPGRADE_FROM_OCS_REGISTRY_IMAGE:-quay.io/ocs-dev/ocs-registry:4.2.0}"
UPGRADE_FROM_OCS_SUBSCRIPTION_CHANNEL="${UPGRADE_FROM_OCS_SUBSCRIPTION_CHANNEL:-$OCS_SUBSCRIPTION_CHANNEL}"

OCS_MUST_GATHER_DIR="${OCS_MUST_GATHER_DIR:-ocs-must-gather}"
OCP_MUST_GATHER_DIR="${OCP_MUST_GATHER_DIR:-ocp-must-gather}"

# Protobuf
PROTOC_VERSION="3.20.0"
PROTOC_GEN_GO_VERSION="1.26.0"
PROTOC_GEN_GO_GRPC_VERSION="1.1.0"

GRPC_BIN="${LOCALBIN}/grpc"
PROTOC="${GRPC_BIN}/protoc"
PROTO_GOOGLE="${GRPC_BIN}/google/protobuf"
PROTOC_GEN_GO="${GRPC_BIN}/protoc-gen-go"
PROTOC_GEN_GO_GRPC="${GRPC_BIN}/protoc-gen-go-grpc"

# gRPC services
SERVICES=("provider")
