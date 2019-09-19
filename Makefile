IMAGE_BUILD_CMD ?= "docker"
IMAGE_REGISTRY ?= "quay.io"
REGISTRY_NAMESPACE ?= "ocs-dev"
IMAGE_TAG ?= "latest"

OUTPUT_DIR="build/_output"
CACHE_DIR="_cache"
TOOLS_DIR="$(CACHE_DIR)/tools"

OPERATOR_SDK_VERSION="v0.10.0"
OPERATOR_SDK_PLATFORM ?= "x86_64-linux-gnu"
OPERATOR_SDK_BIN="operator-sdk-$(OPERATOR_SDK_VERSION)-$(OPERATOR_SDK_PLATFORM)"
OPERATOR_SDK="$(TOOLS_DIR)/$(OPERATOR_SDK_BIN)"

# Export GO111MODULE=on to enable project to be built from within GOPATH/src
export GO111MODULE=on
# Enable GOPROXY. This speeds up a lot of vendoring operations.
export GOPROXY=https://proxy.golang.org
# Export GOROOT. Required for OPERATOR_SDK to work correctly for generate commands.
export GOROOT=$(shell go env GOROOT)

all: ocs-operator ocs-must-gather ocs-registry

.PHONY: clean ocs-operator ocs-must-gather ocs-registry gen-release-csv gen-latest-csv source-manifests \
	cluster-deploy cluster-clean ocs-operator-openshift-ci-build functest update-generated

deps-update:
	go mod tidy && go mod vendor

operator-sdk:
	@if [ ! -x "$(OPERATOR_SDK)" ]; then\
		echo "Downloading operator-sdk $(OPERATOR_SDK_VERSION)";\
		mkdir -p $(TOOLS_DIR);\
		curl -JL https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/$(OPERATOR_SDK_BIN) -o $(OPERATOR_SDK);\
		chmod +x $(OPERATOR_SDK);\
	else\
		echo "Using operator-sdk cached at $(OPERATOR_SDK)";\
	fi

ocs-operator: operator-sdk
	@echo "Building the ocs-operator image"
	$(OPERATOR_SDK) build --go-build-args="-mod=vendor" --image-builder=$(IMAGE_BUILD_CMD) ${IMAGE_REGISTRY}/$(REGISTRY_NAMESPACE)/ocs-operator:$(IMAGE_TAG)

ocs-must-gather:
	@echo "Building the ocs-must-gather image"
	$(IMAGE_BUILD_CMD) build -f must-gather/Dockerfile -t $(IMAGE_REGISTRY)/$(REGISTRY_NAMESPACE)/ocs-must-gather:$(IMAGE_TAG) must-gather/

source-manifests:
	@echo "Sourcing CSV and CRD manifests from component-level operators"
	hack/source-manifests.sh

gen-latest-csv:
	@echo "Generating latest development CSV version using predefined ENV VARs."
	hack/generate-latest-csv.sh

gen-release-csv:
	@echo "Generating unified CSV from sourced component-level operators"
	hack/generate-unified-csv.sh

ocs-registry:
	@echo "Building the ocs-registry image"
	IMAGE_BUILD_CMD=$(IMAGE_BUILD_CMD) REGISTRY_NAMESPACE=$(REGISTRY_NAMESPACE) IMAGE_TAG=$(IMAGE_TAG) IMAGE_REGISTRY=$(IMAGE_REGISTRY) ./hack/build-registry-bundle.sh

clean:
	@echo "cleaning previous outputs"
	rm -rf $(OUTPUT_DIR)

cluster-deploy: cluster-clean
	@echo "Deploying ocs to cluster"
	REGISTRY_NAMESPACE=$(REGISTRY_NAMESPACE) IMAGE_TAG=$(IMAGE_TAG) IMAGE_REGISTRY=$(IMAGE_REGISTRY) ./hack/cluster-deploy.sh

cluster-clean:
	@echo "Removing ocs install from cluster"
	REGISTRY_NAMESPACE=$(REGISTRY_NAMESPACE) IMAGE_TAG=$(IMAGE_TAG) IMAGE_REGISTRY=$(IMAGE_REGISTRY) ./hack/cluster-clean.sh

build-functest:
	@echo "Building functional tests"
	hack/build-functest.sh

functest: build-functest
	@echo "Running functional test suite"
	hack/functest.sh

# This is used by the openshift-ci prow job.
# It just makes the ocs-binary without invoking docker to build the container image.
ocs-operator-openshift-ci-build:
	@echo "Building ocs-operator binary for openshift-ci job"
	mkdir -p build/_output/bin
	go build -mod=vendor -o build/_output/bin/ocs-operator cmd/manager/main.go

update-generated: operator-sdk
	@echo Updating generated files
	@echo
	@$(OPERATOR_SDK) generate k8s
	@echo
	@$(OPERATOR_SDK) generate openapi
