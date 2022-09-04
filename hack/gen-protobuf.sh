#!/usr/bin/env bash

set -o pipefail

source hack/common.sh


mkdir -p ${OUTDIR} ${OUTDIR_GRPC} ${OUTDIR_PROTO_DIST} ${OUTDIR_PROTO_GOOGLE}

EXISTING_PROTOC=$(${OUTDIR_GRPC}/protoc --version 2> /dev/null)
EXISTING_PROTOC_GEN_GO=$(${OUTDIR_GRPC}/protoc-gen-go --version 2>&1 | grep protoc-gen-go)
EXISTING_PROTOC_GEN_GO_GRPC=$(${OUTDIR_GRPC}/protoc-gen-go-grpc --version 2> /dev/null)

if [[ $EXISTING_PROTOC != "libprotoc ${PROTOC_VERSION}" ]]; then
   # download protoc
   wget -P ${OUTDIR_PROTO_DIST} --backups=1 \
		https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-x86_64.zip
   unzip -jod ${OUTDIR_GRPC} ${OUTDIR_PROTO_DIST}/protoc-${PROTOC_VERSION}-linux-x86_64.zip bin/protoc

	# extract descriptor.proto and wrappers.proto
	unzip -jod ${OUTDIR_PROTO_GOOGLE} ${OUTDIR_PROTO_DIST}/protoc-${PROTOC_VERSION}-linux-x86_64.zip \
		include/google/protobuf/descriptor.proto \
		include/google/protobuf/wrappers.proto
fi

if [[ $EXISTING_PROTOC_GEN_GO != "protoc-gen-go v${PROTOC_GEN_GO_VERSION}" ]]; then
    # download protoc-gen-go
    wget -P ${OUTDIR_PROTO_DIST} --backups=1 \
		https://github.com/protocolbuffers/protobuf-go/releases/download/v${PROTOC_GEN_GO_VERSION}/protoc-gen-go.v${PROTOC_GEN_GO_VERSION}.linux.386.tar.gz
	tar -C  ${OUTDIR_GRPC} -zxvf ${OUTDIR_PROTO_DIST}/protoc-gen-go.v${PROTOC_GEN_GO_VERSION}.linux.386.tar.gz protoc-gen-go
fi

if [[ $EXISTING_PROTOC_GEN_GO_GRPC != "protoc-gen-go-grpc ${PROTOC_GEN_GO_GRPC_VERSION}" ]]; then
   # download protoc-gen-go-grpc
	wget -P ${OUTDIR_PROTO_DIST} --backups=1 \
		https://github.com/grpc/grpc-go/releases/download/cmd%2Fprotoc-gen-go-grpc%2Fv${PROTOC_GEN_GO_GRPC_VERSION}/protoc-gen-go-grpc.v${PROTOC_GEN_GO_GRPC_VERSION}.linux.386.tar.gz
	tar -C ${OUTDIR_GRPC} -zxvf ${OUTDIR_PROTO_DIST}/protoc-gen-go-grpc.v${PROTOC_GEN_GO_GRPC_VERSION}.linux.386.tar.gz ./protoc-gen-go-grpc
fi


for service in "${SERVICES[@]}"
do
   # generate code
   ./${OUTDIR_GRPC}/protoc  --proto_path="services/${service}/proto" --go_out="services/${service}/pb" --plugin=${OUTDIR_GRPC}/protoc-gen-go  "${service}.proto"
   ./${OUTDIR_GRPC}/protoc  --proto_path="services/${service}/proto" --go-grpc_out="services/${service}/pb" --plugin=${OUTDIR_GRPC}/protoc-gen-go-grpc "${service}.proto"
done
