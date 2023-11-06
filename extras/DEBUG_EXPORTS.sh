#!/usr/bin/env bash

source hack/common.sh

export OPERATOR_NAME="ocs-operator"
export WATCH_NAMESPACE="openshift-storage"
export ROOK_CEPH_IMAGE=$LATEST_ROOK_IMAGE
export CEPH_IMAGE=$LATEST_CEPH_IMAGE
export NOOBAA_CORE_IMAGE=$LATEST_NOOBAA_CORE_IMAGE
export NOOBAA_DB_IMAGE=$LATEST_NOOBAA_DB_IMAGE