#!/bin/bash
resource="cephclusters"

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

# removing previous collector files
rm -rf ${resource}-collector.sh

api_group="ceph.rook.io"
if [ "${api_group}" != "" ]; then 
    base_collection_path="${base_collection_path}/${api_group}"
fi

base_collection_path="${base_collection_path}/${resource}"
mkdir -p "${base_collection_path}"


# Collection jsonpath
collection_jsonpath="{range .items[*]}"
collection_jsonpath="${collection_jsonpath}echo ' -> Fetching dump for {@.metadata.name} cephcluster';"
collection_jsonpath="${collection_jsonpath}timeout 120 oc get -n ${namespace} ${resource} {@.metadata.name} -o yaml > "
collection_jsonpath="${collection_jsonpath}${base_collection_path}/{@.metadata.name}.yaml;"
collection_jsonpath="${collection_jsonpath}{end}"

# echo $collection_jsonpath   

# Generating the collector script
timeout 120 oc get ${resource} -n "${namespace}" -o jsonpath="${collection_jsonpath}" >> ${resource}-collector.sh

# Executing the collector script
chmod +x ${resource}-collector.sh
./${resource}-collector.sh
rm ${resource}-collector.sh
