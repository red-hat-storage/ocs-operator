#!/bin/bash

set -e

CSV_VERSION=4.8.0
source hack/common.sh

CSV_FILE=/manifests/ocs-operator.clusterserviceversion.yaml

source hack/generate-master-csv.sh
