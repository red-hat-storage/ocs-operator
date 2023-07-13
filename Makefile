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

KUSTOMIZE_VERSION=v4.5.2
CONTROLLER_GEN_VERSION=v0.9.2


.PHONY: \
	build \
	gen-protobuf \
	clean \
	ocs-operator \
	operator-bundle \
	verify-operator-bundle \
	gen-latest-csv \
	gen-latest-deploy-yaml \
	gen-latest-prometheus-rules-yamls \
	verify-latest-deploy-yaml \
	verify-latest-csv \
	cluster-deploy \
	cluster-clean \
	functest \
	shellcheck-test \
	golangci-lint \
	update-generated \
	ocs-operator-ci \
	unit-test \
	deps-update

deps-update:
	@echo "Running deps-update"
	go mod tidy && go mod vendor

operator-sdk:
	@echo "Ensuring operator-sdk"
	hack/ensure-operator-sdk.sh

build: deps-update generate gen-protobuf

ocs-operator: build
	@echo "Building the ocs-operator image"
	hack/build-operator.sh

ocs-metrics-exporter: build
	@echo "Building the ocs-metrics-exporter image"
	hack/build-metrics-exporter.sh

gen-protobuf:
	@echo "Generating protobuf files for gRPC services"
	hack/gen-protobuf.sh

gen-latest-csv: operator-sdk manifests kustomize
	@echo "Generating latest development CSV version using predefined ENV VARs."
	hack/generate-latest-csv.sh

gen-release-csv: operator-sdk manifests kustomize
	@echo "Generating unified CSV from sourced component-level operators"
	hack/generate-unified-csv.sh

gen-latest-deploy-yaml:
	@echo "Generating latest deployment yaml file"
	hack/gen-deployment-yaml.sh

gen-latest-prometheus-rules-yamls:
	@echo "Generating latest Prometheus rules yamls"
	hack/gen-promethues-rules.sh

verify-latest-deploy-yaml: gen-latest-deploy-yaml
	@echo "Verifying deployment yaml changes"
	hack/verify-latest-deploy-yaml.sh

verify-latest-csv: gen-latest-csv
	@echo "Verifying latest CSV"
	hack/verify-latest-csv.sh

verify-operator-bundle: operator-sdk
	@echo "Verifying operator bundle"
	hack/verify-operator-bundle.sh

operator-bundle: gen-latest-csv
	@echo "Building ocs operator bundle"
	hack/build-operator-bundle.sh

operator-index:
	@echo "Building ocs index image in sqlite db based format"
	hack/build-operator-index.sh

operator-catalog:
	@echo "Building ocs catalog image in file based catalog format"
	hack/build-operator-catalog.sh

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

lint: ## Run golangci-lint inside a container
	source hack/common.sh; source hack/docker-common.sh; \
	$${IMAGE_BUILD_CMD} run --rm -v $${PROJECT_DIR}:/app:Z -w /app $${GO_LINT_IMG} golangci-lint run ./...

# ignoring the functest dir since it requires an active cluster
# use 'make functest' to run just the functional tests
unit-test:
	@echo "Executing unit tests"
	hack/unit-test.sh

ocs-operator-ci: shellcheck-test golangci-lint unit-test verify-deps verify-generated verify-latest-csv verify-operator-bundle verify-latest-deploy-yaml

# Generate code
generate: controller-gen
	@echo Updating generated code
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	@echo Updating generated manifests
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd:generateEmbeddedObjectMeta=true paths=./api/... webhook paths="./..." output:crd:artifacts:config=config/crd/bases

verify-deps: deps-update
	@echo "Verifying dependency files"
	hack/verify-dependencies.sh

update-generated: generate manifests

verify-generated: update-generated
	@echo "Verifying generated code and manifests"
	hack/verify-generated.sh

# ARGS is used to pass flags
# `make run ARGS="--zap-devel"` is parsed as
# `go run ./main.go --zap-devel`
run: manifests generate
	go fmt ./...
	go vet ./...
	go run ./main.go $(ARGS)

# find or download controller-gen if necessary
controller-gen:
ifneq ($(CONTROLLER_GEN_VERSION), $(shell $(GOBIN)/controller-gen --version | awk '{print $$2}'))
	@{ \
	echo "Installing controller-gen@$(CONTROLLER_GEN_VERSION)" ;\
	set -e ;\
	go install -mod=readonly sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION) ;\
	echo "Installed controller-gen@$(CONTROLLER_GEN_VERSION)" ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which $(GOBIN)/controller-gen)
endif

kustomize:
ifeq (, $(shell which $(GOBIN)/kustomize))
	@{ \
	echo "Installing kustomize/v4@${KUSTOMIZE_VERSION}" ;\
	set -e ;\
	go install -mod=readonly sigs.k8s.io/kustomize/kustomize/v4@${KUSTOMIZE_VERSION} ;\
	echo "Installed kustomize/v4@${KUSTOMIZE_VERSION}" ;\
	}
export KUSTOMIZE=$(GOBIN)/kustomize
else
export KUSTOMIZE=$(shell which $(GOBIN)/kustomize)
endif

install-noobaa: operator-sdk
	@echo "Installing noobaa operator"
	hack/install-noobaa.sh

install-ocs: operator-sdk
	@echo "Installing ocs operator"
	hack/install-ocs.sh
