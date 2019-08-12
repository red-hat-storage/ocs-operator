IMAGE_BUILD_CMD ?= "docker"
REGISTRY_NAMESPACE ?= "ocs-dev"
IMAGE_TAG ?= "latest"

all: ocs-operator ocs-must-gather ocs-registry

.PHONY: clean ocs-operator ocs-must-gather ocs-registry

ocs-operator:
	@echo "Building the ocs-operator image"
	operator-sdk build --image-builder=$(IMAGE_BUILD_CMD) quay.io/$(REGISTRY_NAMESPACE)/ocs-operator:$(IMAGE_TAG)

ocs-must-gather:
	@echo "Building the ocs-must-gather image"
	$(IMAGE_BUILD_CMD) build -f must-gather/Dockerfile -t quay.io/$(REGISTRY_NAMESPACE)/ocs-must-gather:$(IMAGE_TAG) must-gather/

ocs-registry:
	@echo "Building the ocs-registry image"
	IMAGE_BUILD_CMD=$(IMAGE_BUILD_CMD) REGISTRY_NAMESPACE=$(REGISTRY_NAMESPACE) IMAGE_TAG=$(IMAGE_TAG) ./hack/build-registry-bundle.sh

clean:
	@echo "cleaning previous outputs"
	rm -rf build/_output
