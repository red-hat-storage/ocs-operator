#!/bin/bash
resource="deploymentconfigs"

base_collection_path=$1
namespace=$2
if [ "${base_collection_path}" = "" ];then
    echo "Base collection path for ${resource} is not passed. Exiting."
    exit 0
fi

if [ "${namespace}" = "" ];then
    echo "Namespace for ${resource} is not passed. Exiting."
    exit 0
fi

echo " -> Fetching dump of deploymentconfigs"

api_group="apps.openshift.io"
if [ "${api_group}" != "" ]; then 
    base_collection_path="${base_collection_path}/${api_group}"
fi

mkdir -p "${base_collection_path}"
timeout 120 oc get ${resource} -n "${namespace}" -o yaml > "${base_collection_path}"/${resource}.yaml 2>&1
