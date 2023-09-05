#!/bin/bash

set -e

source hack/common.sh


mkdir -p "${LOCALBIN}" "${GRPC_BIN}" "${PROTO_GOOGLE}"

if [[ ${GOHOSTOS} == "linux" ]]; then
	PROTOC_OS="linux"
elif [[ ${GOHOSTOS} == "darwin" ]]; then
	PROTOC_OS="osx"
fi

if [[ ${GOHOSTARCH} == "amd64" ]]; then
	PROTOC_ARCH="x86_64"
elif [[ ${GOHOSTARCH} == "arm64" ]]; then
	PROTOC_ARCH="aarch_64"
fi

PROTOC_ZIP="protoc-${PROTOC_VERSION}-${PROTOC_OS}-${PROTOC_ARCH}.zip"
PROTOC_DL_URL="https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/${PROTOC_ZIP}"

# install protoc
if [ ! -x "${PROTOC}" ] || [ "$(${PROTOC} --version)" != "libprotoc ${PROTOC_VERSION}" ]; then
	echo "Installing protoc at ${PROTOC}"
	echo "Downloading ${PROTOC_DL_URL}"
	curl -LO "${PROTOC_DL_URL}"
	unzip -jod "${GRPC_BIN}" "${PROTOC_ZIP}" bin/protoc
	unzip -jod "${PROTO_GOOGLE}" "${PROTOC_ZIP}" include/google/protobuf/descriptor.proto include/google/protobuf/wrappers.proto
	rm "${PROTOC_ZIP}"
else
	echo "protoc already present at ${PROTOC}"
fi

# install protoc-gen-go
if [ ! -x "${PROTOC_GEN_GO}" ] || [ "$(${PROTOC_GEN_GO} --version)" != "protoc-gen-go v${PROTOC_GEN_GO_VERSION}" ]; then
	echo "Installing protoc-gen-go at ${PROTOC_GEN_GO}"
	GOBIN=${GRPC_BIN} go install google.golang.org/protobuf/cmd/protoc-gen-go@v${PROTOC_GEN_GO_VERSION}
else
	echo "protoc-gen-go already present at ${PROTOC_GEN_GO}"
fi

# install protoc-gen-go-grpc
if [ ! -x "${PROTOC_GEN_GO_GRPC}" ] || [ "$(${PROTOC_GEN_GO_GRPC} --version)" != "protoc-gen-go-grpc ${PROTOC_GEN_GO_GRPC_VERSION}" ]; then
	echo "Installing protoc-gen-go-grpc at ${PROTOC_GEN_GO_GRPC}"
	GOBIN=${GRPC_BIN} go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v${PROTOC_GEN_GO_GRPC_VERSION}
else
	echo "protoc-gen-go-grpc already present at ${PROTOC_GEN_GO_GRPC}"
fi

# generate code
for service in "${SERVICES[@]}"
do
   ${PROTOC}  --proto_path="services/${service}/proto" --go_out="services/${service}/pb" --plugin="${PROTOC_GEN_GO}"  "${service}.proto"
   ${PROTOC}  --proto_path="services/${service}/proto" --go-grpc_out="services/${service}/pb" --plugin="${PROTOC_GEN_GO_GRPC}" "${service}.proto"
done
