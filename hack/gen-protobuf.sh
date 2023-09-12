#!/usr/bin/env bash

set -o pipefail

source hack/common.sh

case "${GOARCH}" in
	"arm64"|"aarch64")
		PROTOC_ARCH="aarch_64"
		;;
	"x86_64"|"amd64")
		PROTOC_ARCH="x86_64"
		;;
	*)
		echo "unknown ARCH=${GOARCH}; using default x86_64/amd64"
		PROTOC_ARCH="x86_64"
		;;
esac

mkdir -p ${OUTDIR} ${OUTDIR_GRPC} ${OUTDIR_PROTO_DIST} ${OUTDIR_PROTO_GOOGLE}
OUTDIR_GRPC_TMP=$(realpath ${OUTDIR_GRPC}/tmp/)

EXISTING_PROTOC=$(${OUTDIR_GRPC}/protoc --version 2> /dev/null)
EXISTING_PROTOC_GEN_GO=$(${OUTDIR_GRPC}/protoc-gen-go --version 2>&1 | grep protoc-gen-go)
EXISTING_PROTOC_GEN_GO_GRPC=$(${OUTDIR_GRPC}/protoc-gen-go-grpc --version 2> /dev/null)

if [[ $EXISTING_PROTOC != "libprotoc ${PROTOC_VERSION}" ]]; then
	# download protoc
	wget -P ${OUTDIR_PROTO_DIST} --backups=1 \
		https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-${PROTOC_ARCH}.zip
	unzip -jod ${OUTDIR_GRPC} ${OUTDIR_PROTO_DIST}/protoc-${PROTOC_VERSION}-linux-${PROTOC_ARCH}.zip bin/protoc

	# extract descriptor.proto and wrappers.proto
	unzip -jod ${OUTDIR_PROTO_GOOGLE} ${OUTDIR_PROTO_DIST}/protoc-${PROTOC_VERSION}-linux-${PROTOC_ARCH}.zip \
		include/google/protobuf/descriptor.proto \
		include/google/protobuf/wrappers.proto
fi

if [[ $EXISTING_PROTOC_GEN_GO != "protoc-gen-go v${PROTOC_GEN_GO_VERSION}" ]]; then
	# install protoc-gen-go
	mkdir -p "${OUTDIR_GRPC_TMP}"
	GOBIN="${OUTDIR_GRPC_TMP}" go install "google.golang.org/protobuf/cmd/protoc-gen-go@v${PROTOC_GEN_GO_VERSION}"
	mv "${OUTDIR_GRPC_TMP}/protoc-gen-go" "${OUTDIR_GRPC}"
	rm -rf "${OUTDIR_GRPC_TMP}"
fi

if [[ $EXISTING_PROTOC_GEN_GO_GRPC != "protoc-gen-go-grpc ${PROTOC_GEN_GO_GRPC_VERSION}" ]]; then
	# install protoc-gen-go-grpc
	mkdir -p "${OUTDIR_GRPC_TMP}"
	GOBIN="${OUTDIR_GRPC_TMP}" go install "google.golang.org/grpc/cmd/protoc-gen-go-grpc@v${PROTOC_GEN_GO_GRPC_VERSION}"
	mv "${OUTDIR_GRPC_TMP}/protoc-gen-go-grpc" "${OUTDIR_GRPC}"
	rm -rf "${OUTDIR_GRPC_TMP}"
fi

for service in "${SERVICES[@]}"
do
	# generate code
	./${OUTDIR_GRPC}/protoc  --proto_path="services/${service}/proto" --go_out="services/${service}/pb" --plugin=${OUTDIR_GRPC}/protoc-gen-go  "${service}.proto"
	./${OUTDIR_GRPC}/protoc  --proto_path="services/${service}/proto" --go-grpc_out="services/${service}/pb" --plugin=${OUTDIR_GRPC}/protoc-gen-go-grpc "${service}.proto"
done

