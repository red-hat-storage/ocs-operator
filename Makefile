# Set minimum required Golang version to v1.13.3
GO_REQUIRED_MIN_VERSION=1.13.3
# Export GO111MODULE=on to enable project to be built from within GOPATH/src
export GO111MODULE=on
# Enable GOPROXY. This speeds up a lot of vendoring operations.
export GOPROXY=https://proxy.golang.org
# Export GOROOT. Required for OPERATOR_SDK to work correctly for generate commands.
export GOROOT=$(shell go env GOROOT)

# Explicitly set controller-gen to v0.2.5 as build-machinery-go only supports v0.2.1 currently
export CONTROLLER_GEN_VERSION=v0.2.5

all: ocs-operator ocs-registry ocs-must-gather

include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
  targets/openshift/crd-schema-gen.mk \
)

$(call add-crd-gen,ocsv1,./pkg/apis/ocs/v1,./deploy/crds,./deploy/crds)

.PHONY: \
	build \
	build-go \
	build-container \
	clean \
	ocs-operator \
	ocs-must-gather \
	operator-bundle \
	verify-operator-bundle \
	operator-index \
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
	unit-test \
	deps-update

deps-update:
	@echo "Running deps-update"
	go mod tidy && go mod vendor

operator-sdk:
	@echo "Ensuring operator-sdk"
	hack/ensure-operator-sdk.sh

ocs-operator-openshift-ci-build: build

build: build-go

build-go: deps-update
	@echo "Building the ocs-operator binary"
	hack/go-build.sh

build-container:
	@echo "Building the ocs-operator binary (containerized)"
	hack/build-container.sh

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

verify-latest-deploy-yaml: gen-latest-deploy-yaml
	@echo "Verifying deployment yaml changes"
	hack/verify-latest-deploy-yaml.sh

gen-release-csv: operator-sdk
	@echo "Generating unified CSV from sourced component-level operators"
	hack/generate-unified-csv.sh

verify-latest-csv: gen-latest-csv
	@echo "Verifying latest CSV"
	hack/verify-latest-csv.sh

verify-operator-bundle:
	@echo "Verifying operator bundle"
	hack/verify-operator-bundle.sh

operator-bundle:
	@echo "Building ocs-operator-bundle image"
	hack/build-operator-bundle.sh

operator-index:
	@echo "Building ocs-operator-index image"
	hack/build-operator-index.sh

ocs-registry:
	@echo "Building ocs-registry image in appregistry format"
	hack/build-appregistry.sh

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
	hack/functest.sh $(ARGS)
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
	go test -v -cover `go list ./... | grep -v "functest"`

update-generated: operator-sdk
	@echo Updating generated files
	hack/generate-k8s-openapi.sh
	@echo Running update-codegen-crds
	@make update-codegen-crds

verify-generated: update-generated
	@echo "Verifying generated code"
	hack/verify-generated.sh

ocs-operator-ci: shellcheck-test gofmt golint govet unit-test verify-generated verify-latest-deploy-yaml

red-hat-storage-ocs-ci:
	@echo "Running red-hat-storage ocs-ci test suite"
	hack/red-hat-storage-ocs-ci-tests.sh
