all: clean ocs-operator

.PHONY: ocs-operator
ocs-operator:
	@echo "Building ocs-operator image"
	operator-sdk build quay.io/openshift/ocs-operator

.PHONY: clean
clean:
	@echo "cleaning previous outputs"
	rm -rf build/_output