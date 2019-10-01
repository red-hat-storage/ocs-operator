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

TARGET_GOOS=linux
TARGET_GOARCH=amd64

# Export GO111MODULE=on to enable project to be built from within GOPATH/src
export GO111MODULE=on
# Enable GOPROXY. This speeds up a lot of vendoring operations.
export GOPROXY=https://proxy.golang.org
# Export GOROOT. Required for OPERATOR_SDK to work correctly for generate commands.
export GOROOT=$(shell go env GOROOT)

all: ocs-operator ocs-must-gather ocs-registry

.PHONY: \
	build \
	clean \
	ocs-operator \
	ocs-must-gather \
	ocs-registry \
	gen-release-csv \
	gen-latest-csv \
	gen-latest-deploy-yaml \
	verify-latest-deploy-yaml \
	verify-latest-csv \
	source-manifests \
	cluster-deploy \
	cluster-clean \
	ocs-operator-openshift-ci-build \
	functest \
	gofmt \
	golint \
	govet \
	update-generated \
	ocs-operator-ci \
	red-hat-storage-ocs-ci \
	unit-test

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

ocs-operator-openshift-ci-build: build

build:
	@echo "Building the ocs-operator binary"
	mkdir -p build/_output/bin
	env GOOS=$(TARGET_GOOS) GOARCH=$(TARGET_GOARCH) go build -i -ldflags="-s -w" -mod=vendor -o build/_output/bin/ocs-operator ./cmd/manager

ocs-operator: build
	@echo "Building the ocs-operator image"
	$(IMAGE_BUILD_CMD) build -f build/Dockerfile -t $(IMAGE_REGISTRY)/$(REGISTRY_NAMESPACE)/ocs-operator:$(IMAGE_TAG) build/

ocs-must-gather:
	@echo "Building the ocs-must-gather image"
	$(IMAGE_BUILD_CMD) build -f must-gather/Dockerfile -t $(IMAGE_REGISTRY)/$(REGISTRY_NAMESPACE)/ocs-must-gather:$(IMAGE_TAG) must-gather/

source-manifests:
	@echo "Sourcing CSV and CRD manifests from component-level operators"
	hack/source-manifests.sh

gen-latest-csv:
	@echo "Generating latest development CSV version using predefined ENV VARs."
	hack/generate-latest-csv.sh

gen-latest-deploy-yaml:
	@echo "Generating latest deployment yaml file"
	hack/gen-deployment-yaml.sh

verify-latest-deploy-yaml:
	@echo "Verifying deployment yaml changes"
	hack/verify-latest-deploy-yaml.sh

gen-release-csv:
	@echo "Generating unified CSV from sourced component-level operators"
	hack/generate-unified-csv.sh

verify-latest-csv:
	@echo "Verifying latest csv checksum"
	hack/verify-latest-csv.sh

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
	@echo "Running ocs developer functional test suite"
	hack/functest.sh
gofmt:
	@echo "Running gofmt"
	gofmt -s -l `find . -path ./vendor -prune -o -type f -name '*.go' -print`

golint:
	@echo "Running go lint"
	hack/lint.sh

govet:
	@echo "Running go vet"
	go vet ./...

# ignoring the functest dir since it requires an active cluster
# use 'make functest' to run just the functional tests
unit-test:
	@echo "Executing unit tests"
	go test -v `go list ./... | grep -v "functest"`

update-generated: operator-sdk
	@echo Updating generated files
	@echo
	@$(OPERATOR_SDK) generate k8s
	@echo
	@$(OPERATOR_SDK) generate openapi

verify-generated: update-generated
	@echo "Verifying generated code"
	hack/verify-generated.sh

ocs-operator-ci: gofmt golint govet unit-test build verify-latest-csv verify-generated verify-latest-deploy-yaml

red-hat-storage-ocs-ci:
	@echo "Running red-hat-storage ocs-ci test suite"
	hack/red-hat-storage-ocs-ci-tests.sh
