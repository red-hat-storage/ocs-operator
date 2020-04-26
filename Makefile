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
	shellcheck-test \
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
	@echo "Ensuring operator-sdk"
	hack/ensure-operator-sdk.sh

ocs-operator-openshift-ci-build: build

build:
	@echo "Building the ocs-operator binary"
	mkdir -p build/_output/bin
	env GOOS=$(TARGET_GOOS) GOARCH=$(TARGET_GOARCH) go build -i -ldflags="-s -w" -mod=vendor -o build/_output/bin/ocs-operator ./cmd/manager

ocs-operator: build
	@echo "Building the ocs-operator image"
	hack/build-operator.sh

ocs-must-gather:
	@echo "Building the ocs-must-gather image"
	hack/build-must-gather.sh

source-manifests: operator-sdk
	@echo "Sourcing CSV and CRD manifests from component-level operators"
	hack/source-manifests.sh

gen-latest-csv: operator-sdk
	@echo "Generating latest development CSV version using predefined ENV VARs."
	hack/generate-latest-csv.sh

gen-latest-deploy-yaml:
	@echo "Generating latest deployment yaml file"
	hack/gen-deployment-yaml.sh

verify-latest-deploy-yaml:
	@echo "Verifying deployment yaml changes"
	hack/verify-latest-deploy-yaml.sh

gen-release-csv: operator-sdk
	@echo "Generating unified CSV from sourced component-level operators"
	hack/generate-unified-csv.sh

verify-latest-csv:
	@echo "Verifying latest csv checksum"
	hack/verify-latest-csv.sh

ocs-registry:
	@echo "Building the ocs-registry image"
	hack/build-registry-bundle.sh

clean:
	@echo "cleaning previous outputs"
	hack/clean.sh

cluster-deploy: cluster-clean
	@echo "Deploying ocs to cluster"
	hack/cluster-deploy.sh

cluster-clean:
	@echo "Removing ocs install from cluster"
	hack/cluster-clean.sh

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

shellcheck-test:
	@echo "Testing for shellcheck"
	hack/shellcheck-test.sh

# ignoring the functest dir since it requires an active cluster
# use 'make functest' to run just the functional tests
unit-test:
	@echo "Executing unit tests"
	go test -v `go list ./... | grep -v "functest"`

update-generated: operator-sdk
	@echo Updating generated files
	hack/generate-k8s-openapi.sh

verify-generated: update-generated
	@echo "Verifying generated code"
	hack/verify-generated.sh

ocs-operator-ci: shellcheck-test gofmt golint govet unit-test build verify-generated verify-latest-deploy-yaml

red-hat-storage-ocs-ci:
	@echo "Running red-hat-storage ocs-ci test suite"
	hack/red-hat-storage-ocs-ci-tests.sh
