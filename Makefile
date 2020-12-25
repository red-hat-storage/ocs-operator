# Export GO111MODULE=on to enable project to be built from within GOPATH/src
export GO111MODULE=on
# Enable GOPROXY. This speeds up a lot of vendoring operations.
export GOPROXY=https://proxy.golang.org
# Export GOROOT. Required for OPERATOR_SDK to work correctly for generate commands.
export GOROOT=$(shell go env GOROOT)
# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

CONTROLLER_GEN_VERSION=v0.4.1
CRD_OPTIONS ?= "crd:trivialVersions=true"

all: ocs-operator ocs-registry ocs-must-gather

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
	gen-latest-prometheus-rules-yamls \
	verify-latest-deploy-yaml \
	verify-latest-csv \
	source-manifests \
	cluster-deploy \
	cluster-clean \
	ocs-operator-openshift-ci-build \
	functest \
	shellcheck-test \
	golangci-lint \
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

build-go: deps-update generate
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

source-manifests: operator-sdk manifests kustomize
	@echo "Sourcing CSV and CRD manifests from component-level operators"
	hack/source-manifests.sh

gen-latest-csv: operator-sdk manifests kustomize
	@echo "Generating latest development CSV version using predefined ENV VARs."
	hack/generate-latest-csv.sh

gen-latest-deploy-yaml:
	@echo "Generating latest deployment yaml file"
	hack/gen-deployment-yaml.sh

gen-latest-prometheus-rules-yamls:
	@echo "Generating latest Prometheus rules yamls"
	hack/gen-promethues-rules.sh

verify-latest-deploy-yaml: gen-latest-deploy-yaml
	@echo "Verifying deployment yaml changes"
	hack/verify-latest-deploy-yaml.sh

gen-release-csv: operator-sdk manifests kustomize
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

shellcheck-test:
	@echo "Testing for shellcheck"
	hack/shellcheck-test.sh

golangci-lint:
	@echo "Running golangci-lint run"
	hack/golangci_lint.sh

# ignoring the functest dir since it requires an active cluster
# use 'make functest' to run just the functional tests
unit-test:
	@echo "Executing unit tests"
	go test -v -cover `go list ./... | grep -v "functest"`

ocs-operator-ci: shellcheck-test golangci-lint unit-test verify-generated verify-latest-deploy-yaml

red-hat-storage-ocs-ci:
	@echo "Running red-hat-storage ocs-ci test suite"
	hack/red-hat-storage-ocs-ci-tests.sh

# Generate code
generate: controller-gen
	@echo Updating generated code
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	@echo Updating generated manifests
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

update-generated: generate manifests

verify-generated: update-generated
	@echo "Verifying generated code and manifests"
	hack/verify-generated.sh

# find or download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION) ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

kustomize:
ifeq (, $(shell which kustomize))
	@{ \
	set -e ;\
	KUSTOMIZE_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$KUSTOMIZE_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/kustomize/kustomize/v3@v3.5.4 ;\
	rm -rf $$KUSTOMIZE_GEN_TMP_DIR ;\
	}
export KUSTOMIZE=$(GOBIN)/kustomize
else
export KUSTOMIZE=$(shell which kustomize)
endif