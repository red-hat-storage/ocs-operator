#!/bin/bash

source hack/common.sh

$OUTDIR_BIN/functests --ocs-registry-image="${FULL_IMAGE_NAME}" --local-storage-registry-image="${LOCAL_STORAGE_IMAGE_NAME}"  $@
if [ $? -ne 0 ]; then
	hack/dump-debug-info.sh
	echo "ERROR: Functest failed."
	exit 1
fi

