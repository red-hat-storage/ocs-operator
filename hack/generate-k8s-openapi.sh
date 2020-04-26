#!/bin/bash

set -e

source hack/common.sh
source hack/operator-sdk-common.sh

./${OPERATOR_SDK} generate k8s
./${OPERATOR_SDK} generate crds
