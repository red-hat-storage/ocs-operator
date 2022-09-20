#!/usr/bin/env bash

set -e

CSV_VERSION=4.12.0
source hack/generate-appregistry.sh

CSV_FILE=build/_output/appregistry/olm-catalog/ocs-operator/${CSV_VERSION}/ocs-operator.v${CSV_VERSION}.clusterserviceversion.yaml

source hack/generate-master-csv.sh
