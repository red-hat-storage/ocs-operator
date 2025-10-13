#!/usr/bin/env bash

# shellcheck disable=SC2034
# disable unused variable warnings

TARGET_OS="${TARGET_OS:-linux}"
TARGET_ARCH="${TARGET_ARCH:-amd64}"
HOST_OS="$(go env GOHOSTOS)"
HOST_ARCH="$(go env GOHOSTARCH)"

# Current DEV version of the CSV
DEFAULT_CSV_VERSION="4.20.0"
CSV_VERSION="${CSV_VERSION:-${DEFAULT_CSV_VERSION}}"
VERSION="${VERSION:-${CSV_VERSION}}"
LDFLAGS="-X github.com/red-hat-storage/ocs-operator/v4/version.Version=${CSV_VERSION}"

# Vars for CSV generation
OUTDIR_TEMPLATES="deploy/csv-templates"
OCS_CSV="$OUTDIR_TEMPLATES/ocs-operator.csv.yaml.in"
OUTDIR_CRDS="$OUTDIR_TEMPLATES/crds"
OCS_FINAL_DIR="deploy/ocs-operator/manifests"
BUNDLEMANIFESTS_DIR="rbac"

# External images used in CSV generation
LATEST_ROOK_IMAGE="quay.io/ocs-dev/rook-ceph:vmaster-b0d17ebba" # Using downstream rook image as it contains downstream-only changes
LATEST_CEPH_IMAGE="quay.io/ceph/ceph:v19.2.3" # Ref-https://github.com/rook/rook/blob/master/deploy/examples/images.txt#L3
LATEST_NOOBAA_CORE_IMAGE="quay.io/noobaa/noobaa-core:master-20250730"
LATEST_NOOBAA_DB_IMAGE="quay.io/sclorg/postgresql-15-c9s" # Ref-https://github.com/noobaa/noobaa-operator/blob/5da5c26e9f126c488445d7d9f9326cf676bdd0ea/pkg/options/options.go#L73-L75
KUBE_RBAC_PROXY_FULL_IMAGE_NAME="gcr.io/kubebuilder/kube-rbac-proxy:v0.13.1"
UX_BACKEND_OAUTH_FULL_IMAGE_NAME="quay.io/openshift/origin-oauth-proxy:4.20.0"
LATEST_MUST_GATHER_IMAGE="quay.io/ocs-dev/ocs-must-gather:latest"

# Images built in our repo
IMAGE_REGISTRY="${IMAGE_REGISTRY:=quay.io}"
REGISTRY_NAMESPACE="${REGISTRY_NAMESPACE:=ocs-dev}"
IMAGE_TAG="${IMAGE_TAG:=latest}"

OPERATOR_IMAGE_NAME="${OPERATOR_IMAGE_NAME:=ocs-operator}"
METRICS_EXPORTER_IMAGE_NAME="${METRICS_EXPORTER_IMAGE_NAME:=ocs-metrics-exporter}"
OPERATOR_BUNDLE_IMAGE_NAME="${OPERATOR_BUNDLE_IMAGE_NAME:=ocs-operator-bundle}"

OPERATOR_FULL_IMAGE_NAME="${OPERATOR_FULL_IMAGE_NAME:=${IMAGE_REGISTRY}/${REGISTRY_NAMESPACE}/${OPERATOR_IMAGE_NAME}:${IMAGE_TAG}}"
METRICS_EXPORTER_FULL_IMAGE_NAME="${METRICS_EXPORTER_FULL_IMAGE_NAME:=${IMAGE_REGISTRY}/${REGISTRY_NAMESPACE}/${METRICS_EXPORTER_IMAGE_NAME}:${IMAGE_TAG}}"
BUNDLE_FULL_IMAGE_NAME="${BUNDLE_FULL_IMAGE_NAME:=${IMAGE_REGISTRY}/${REGISTRY_NAMESPACE}/${OPERATOR_BUNDLE_IMAGE_NAME}:${IMAGE_TAG}}"

# Bundle images of Dependencies of OCS Operator
NOOBAA_BUNDLE_FULL_IMAGE_NAME="quay.io/noobaa/noobaa-operator-bundle:master-20250730"
ROOK_BUNDLE_FULL_IMAGE_NAME="quay.io/ocs-dev/rook-ceph-operator-bundle:master-b0d17ebba"
OCS_CLIENT_BUNDLE_FULL_IMAGE_NAME="quay.io/ocs-dev/ocs-client-operator-bundle:main-53749f"
# Below bundles are dependencies of ocs-client-operator, Ref- https://github.com/red-hat-storage/ocs-client-operator/blob/main/bundle/metadata/dependencies.yaml
CSI_ADDONS_BUNDLE_FULL_IMAGE_NAME="quay.io/csiaddons/k8s-bundle:v0.12.0"
CEPH_CSI_BUNDLE_FULL_IMAGE_NAME="quay.io/ocs-dev/cephcsi-operator-bundle:main-f73fca8"
RECIPE_BUNDLE_FULL_IMAGE_NAME="quay.io/ramendr/recipe-bundle:latest"
SNAPSHOT_CONTROLLER_BUNDLE_FULL_IMAGE_NAME="quay.io/ocs-dev/snapshot-controller-bundle:main-dfea221"

# Vars for testing
GINKGO_TEST_SUITE="${GINKGO_TEST_SUITE:-ocs}"

OCS_OPERATOR_INSTALL="${OCS_OPERATOR_INSTALL:-false}"
OCS_CLUSTER_UNINSTALL="${OCS_CLUSTER_UNINSTALL:-false}"
OCS_SUBSCRIPTION_CHANNEL=${OCS_SUBSCRIPTION_CHANNEL:-alpha}
INSTALL_NAMESPACE="${INSTALL_NAMESPACE:-openshift-storage}"
UPGRADE_FROM_OCS_REGISTRY_IMAGE="${UPGRADE_FROM_OCS_REGISTRY_IMAGE:-quay.io/ocs-dev/ocs-registry:4.2.0}"
UPGRADE_FROM_OCS_SUBSCRIPTION_CHANNEL="${UPGRADE_FROM_OCS_SUBSCRIPTION_CHANNEL:-$OCS_SUBSCRIPTION_CHANNEL}"

OCS_MUST_GATHER_DIR="${OCS_MUST_GATHER_DIR:-ocs-must-gather}"

# Tools & binaries versions and locations
LOCALBIN="$(pwd)/bin"
OPERATOR_SDK_VERSION="v1.30.0"
OPERATOR_SDK="${LOCALBIN}/operator-sdk-${OPERATOR_SDK_VERSION}"
GINKGO="${LOCALBIN}/ginkgo"
GOLANGCI_LINT_VERSION="v1.64.8"
GOLANGCI_LINT="${LOCALBIN}/golangci-lint"
SHELLCHECK_VERSION="v0.9.0"
SHELLCHECK="${LOCALBIN}/shellcheck"

OCS_OC_PATH="${OCS_OC_PATH:-oc}"

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
