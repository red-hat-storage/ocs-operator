#!/bin/bash
resource="pods"

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
rm -rf collector.sh

api_group="core"
if [ "${api_group}" != "" ]; then 
    core_base_collection_path="${base_collection_path}/${api_group}"
fi

core_base_collection_path="${core_base_collection_path}/${resource}"
mkdir -p "${core_base_collection_path}"
timeout 120 oc get ${resource} -n "${namespace}" -o yaml > "${core_base_collection_path}"/${resource}.yaml 2>&1

base_collection_path="${base_collection_path}/${resource}"
mkdir -p "${base_collection_path}"

function dump_pod {
    pod_name=${1}
    touch "${pod_name}"-init-collector.sh && chmod +x "${pod_name}"-init-collector.sh        
    touch "${pod_name}"-collector.sh && chmod +x "${pod_name}"-collector.sh
    # Executing the collector script

    # Collection jsonpath for init containers
    initcontainer_collection_jsonpath="{range @.spec.initContainers[*]}"
    initcontainer_collection_jsonpath="${initcontainer_collection_jsonpath}mkdir -p ${base_collection_path}/${pod_name}/{@.name}/logs && "
    initcontainer_collection_jsonpath="${initcontainer_collection_jsonpath}timeout 120 oc logs ${pod_name} -n ${namespace} -c {@.name} > ${base_collection_path}/${pod_name}/{@.name}/logs/current.log 2>&1 & "
    initcontainer_collection_jsonpath="${initcontainer_collection_jsonpath}mkdir -p ${base_collection_path}/${pod_name}/{@.name}/logs && "
    initcontainer_collection_jsonpath="${initcontainer_collection_jsonpath}timeout 120 oc logs -p ${pod_name} -n ${namespace} -c {@.name} > ${base_collection_path}/${pod_name}/{@.name}/logs/previous.log 2>&1 & "
    initcontainer_collection_jsonpath="${initcontainer_collection_jsonpath}{end}"
    
    # Collection jsonpath for containers
    container_collection_jsonpath="{range @.spec.containers[*]}"
    container_collection_jsonpath="${container_collection_jsonpath}mkdir -p ${base_collection_path}/${pod_name}/{@.name}/logs && "
    container_collection_jsonpath="${container_collection_jsonpath}timeout 120 oc logs ${pod_name} -n ${namespace} -c {@.name} > ${base_collection_path}/${pod_name}/{@.name}/logs/current.log 2>&1 & "
    container_collection_jsonpath="${container_collection_jsonpath}mkdir -p ${base_collection_path}/${pod_name}/{@.name}/logs && "
    container_collection_jsonpath="${container_collection_jsonpath}timeout 120 oc logs -p ${pod_name} -n ${namespace} -c {@.name} > ${base_collection_path}/${pod_name}/{@.name}/logs/previous.log 2>&1 & "
    container_collection_jsonpath="${container_collection_jsonpath}{end}"

    timeout 120 oc get pods "${pod_name}" -n "${namespace}" -o jsonpath="${initcontainer_collection_jsonpath}" >> "${pod_name}"-init-collector.sh & 
    timeout 120 oc get pods "${pod_name}" -n "${namespace}" -o jsonpath="${container_collection_jsonpath}" >> "${pod_name}"-collector.sh &
    wait
    echo "wait" >> "${pod_name}"-init-collector.sh
    echo "wait" >> "${pod_name}"-collector.sh
    ./"${pod_name}"-init-collector.sh &
    ./"${pod_name}"-collector.sh &
    wait
    rm "${pod_name}"-init-collector.sh
    rm "${pod_name}"-collector.sh
}

# Generating the collector script
cpu_numbers=$(nproc 2>/dev/null || echo 4)
for pod in $(timeout 120 oc get pods --no-headers -n "${namespace}" | awk '{print $1}'); do
    while true; do
        if [ "$(jobs -rp | wc -l)" -lt "${cpu_numbers}" ]; then break; fi
        sleep 3
    done
    echo " -> Fetching dump for ${pod} pod"
    mkdir -p "${base_collection_path}"/"${pod}"
    timeout 120 oc get ${resource} "${pod}" -n "${namespace}" -o yaml > "${base_collection_path}"/"${pod}"/"${pod}".yaml 2>&1 &
    timeout 120 oc describe ${resource} "${pod}" -n "${namespace}" > "${base_collection_path}"/"${pod}"/describe.yaml 2>&1 &
    dump_pod "${pod}" &
done
wait
