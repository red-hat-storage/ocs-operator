LOCALBIN=$(shell pwd)/bin

KUSTOMIZE_VERSION=v4.5.7
KUSTOMIZE=$(LOCALBIN)/kustomize
CONTROLLER_GEN_VERSION=v0.17.0
CONTROLLER_GEN=$(LOCALBIN)/controller-gen

# Container command detection (podman or docker)
CONTAINER_CMD ?= $(shell command -v podman 2>/dev/null || command -v docker 2>/dev/null)
GOARCH ?= $(shell go env GOARCH 2>/dev/null)
METRICS_DEVEL_IMAGE = ocs-metrics-exporter:devel

.PHONY: \
	build \
	gen-protobuf \
	clean \
	ocs-operator \
	operator-bundle \
	verify-operator-bundle \
	gen-latest-csv \
	gen-latest-prometheus-rules-yamls \
	verify-latest-csv \
	functest \
	shellcheck-test \
	golangci-lint \
	update-generated \
	ocs-operator-ci \
	unit-test \
	deps-update \
	containerized-metrics-build \
	containerized-metrics-test

deps-update:
	set -e
	@echo "Running go mod tidy on root module"
	cd . && go mod tidy
	@echo "Running go mod tidy on api submodule"
	cd api && go mod tidy
	@echo "Running go mod tidy on metrics submodule"
	cd metrics && go mod tidy
	@echo "Running go mod tidy on provider/api submodule"
	cd services/provider/api && go mod tidy
	@echo "Running go work sync"
	go work sync
	@echo "Done"


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

# Build the devel container with ceph C headers for metrics exporter
.metrics-devel-container-id: metrics/Dockerfile.devel
	@test -n "$(CONTAINER_CMD)" || { echo "podman or docker not found"; exit 1; }
	[ ! -f .metrics-devel-container-id ] || $(CONTAINER_CMD) rmi $(METRICS_DEVEL_IMAGE) 2>/dev/null || true
	$(RM) .metrics-devel-container-id
	$(CONTAINER_CMD) build --build-arg GOARCH=$(GOARCH) -t $(METRICS_DEVEL_IMAGE) -f metrics/Dockerfile.devel .
	$(CONTAINER_CMD) inspect -f '{{.Id}}' $(METRICS_DEVEL_IMAGE) > .metrics-devel-container-id

# Build metrics exporter inside a container (provides ceph C headers for go-ceph CGO)
containerized-metrics-build: .metrics-devel-container-id
	$(CONTAINER_CMD) run --rm -v $(CURDIR):/workspace $(METRICS_DEVEL_IMAGE) \
		go build ./metrics/...

# Run metrics exporter tests inside a container
containerized-metrics-test: .metrics-devel-container-id
	$(CONTAINER_CMD) run --rm -v $(CURDIR):/workspace $(METRICS_DEVEL_IMAGE) \
		go test -v -cover ./metrics/...

gen-protobuf:
	@echo "Generating protobuf files for gRPC services"
	hack/gen-protobuf.sh

gen-latest-csv: operator-sdk manifests kustomize
	@echo "Generating latest development CSV version using predefined ENV VARs."
	hack/generate-latest-csv.sh

gen-release-csv: operator-sdk manifests kustomize
	@echo "Generating unified CSV from sourced component-level operators"
	hack/generate-unified-csv.sh

verify-latest-csv: gen-latest-csv
	@echo "Verifying latest CSV"
	hack/verify-latest-csv.sh

verify-operator-bundle: operator-sdk
	@echo "Verifying operator bundle"
	hack/verify-operator-bundle.sh

operator-bundle: gen-latest-csv
	@echo "Building ocs operator bundle"
	hack/build-operator-bundle.sh

clean:
	@echo "cleaning previous outputs"
	hack/clean.sh

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
	hack/unit-test.sh

ocs-operator-ci: shellcheck-test golangci-lint unit-test verify-deps verify-generated verify-latest-csv verify-operator-bundle

# Generate code
# Explicit paths exclude metrics/ which uses go-ceph CGO and breaks controller-gen.
generate: controller-gen
	@echo Updating generated code
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..." paths="./cmd/..." paths="./internal/..." paths="./pkg/..." paths="./services/..."

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	@echo Updating generated manifests
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd:generateEmbeddedObjectMeta=true,allowDangerousTypes=true webhook paths="./api/..." paths="./cmd/..." paths="./internal/..." paths="./pkg/..." paths="./services/..." output:crd:artifacts:config=config/crd/bases

verify-deps: deps-update
	@echo "Verifying dependency files"
	hack/verify-dependencies.sh

update-generated: generate manifests

verify-generated: update-generated
	@echo "Verifying generated code and manifests"
	hack/verify-generated.sh

# ARGS is used to pass flags
# `make run ARGS="--zap-devel"` is parsed as
# `go run ./cmd/main.go --zap-devel`
run: manifests generate
	go fmt ./...
	go vet ./...
	go run ./cmd/main.go $(ARGS)

# find or download controller-gen if necessary
controller-gen:
ifeq ($(wildcard ${CONTROLLER_GEN}),)
	@echo "Installing controller-gen@${CONTROLLER_GEN_VERSION} at ${CONTROLLER_GEN}"
	@GOBIN=$(LOCALBIN) go install -mod=readonly sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)
else ifneq ($(shell ${CONTROLLER_GEN} --version | awk '{print $$2}'), $(CONTROLLER_GEN_VERSION))
	@echo "Installing newer controller-gen@${CONTROLLER_GEN_VERSION} at ${CONTROLLER_GEN}"
	@GOBIN=$(LOCALBIN) go install -mod=readonly sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)
else
	@echo "Using existing controller-gen@${CONTROLLER_GEN_VERSION} at ${CONTROLLER_GEN}"
endif

# find or download kustomize if necessary
kustomize:
ifeq ($(wildcard ${KUSTOMIZE}),)
	@echo "Installing kustomize@${KUSTOMIZE_VERSION} at ${KUSTOMIZE}"
	@curl -s "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"  | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN)
else ifneq ($(shell ${KUSTOMIZE} version | awk -F'[ /]' '/Version/{print $$2}'), $(KUSTOMIZE_VERSION))
	@echo "Installing newer kustomize@${KUSTOMIZE_VERSION} at ${KUSTOMIZE}"
	@rm -f ${KUSTOMIZE}
	@curl -s "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"  | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN)
else
	@echo "Using existing kustomize@${KUSTOMIZE_VERSION} at ${KUSTOMIZE}"
endif
export KUSTOMIZE

install: operator-sdk
	@echo "Installing operators"
	hack/install-ocs-operator.sh
