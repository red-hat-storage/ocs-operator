all: clean ocs-operator ocs-must-gather

.PHONY: ocs-operator
ocs-operator:
	@echo "Building ocs-operator image"
	operator-sdk build quay.io/openshift/ocs-operator

.PHONY: ocs-must-gather
ocs-must-gather:
	@echo "Building ocs-must-gather image"
	docker build -f must-gather/Dockerfile -t quay.io/openshift/ocs-must-gather:latest must-gather/

.PHONY: clean
clean:
	@echo "cleaning previous outputs"
	rm -rf build/_output