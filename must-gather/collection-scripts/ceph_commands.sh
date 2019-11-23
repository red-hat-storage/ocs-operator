#!/bin/bash
base_collection_path=$1
namespace=$2
if [ "${base_collection_path}" = "" ];then
    echo "Base collection path for ceph commands is not passed. Exiting."
    exit 0
fi

if [ "${namespace}" = "" ];then
    echo "Namespace for ceph commands is not passed. Exiting."
    exit 0
fi

# Ceph commands
ceph_commands=()
ceph_commands+=("ceph status")
ceph_commands+=("ceph health detail")
ceph_commands+=("ceph osd tree")
ceph_commands+=("ceph osd stat")
ceph_commands+=("ceph osd dump")
ceph_commands+=("ceph mon stat")
ceph_commands+=("ceph mon dump")
ceph_commands+=("ceph df")
ceph_commands+=("ceph report")
ceph_commands+=("ceph osd df tree")
ceph_commands+=("ceph fs ls")
ceph_commands+=("ceph pg dump")
ceph_commands+=("ceph osd crush show-tunables")
ceph_commands+=("ceph osd crush dump")
ceph_commands+=("ceph mgr dump")
ceph_commands+=("ceph mds stat")
ceph_commands+=("ceph versions")
ceph_commands+=("ceph fs dump")
ceph_commands+=("ceph auth list")

# Ceph volume commands
ceph_volume_commands+=()
ceph_volume_commands+=("ceph-volume lvm list")

ceph_commands_output_dir="${base_collection_path}/must_gather_commands"
mkdir -p "${ceph_commands_output_dir}"
ceph_commands_json_output_dir="${ceph_commands_output_dir}/json_output"
mkdir -p "${ceph_commands_json_output_dir}"

# Collecting output of ceph commands
cpu_numbers=$(nproc 2>/dev/null || echo 4)
helper_pod_status=$(timeout 120 oc get pods "${HOSTNAME}"-helper -n "${namespace}" -o jsonpath="{.status.phase}")
if [ "${helper_pod_status}" = "Running" ]; then
    for ((i = 0; i < ${#ceph_commands[@]}; i++)); do
        while true; do
            if [ "$(jobs -rp | wc -l)" -lt "${cpu_numbers}" ]; then break; fi
            sleep 3
        done
        printf " -> Fetching output for '%s' from %s pod\n"  "${ceph_commands[$i]}" "${HOSTNAME}-helper"
        ceph_command_output_file=${ceph_commands_output_dir}/${ceph_commands[$i]// /_}
        ceph_command_json_output_file=${ceph_commands_json_output_dir}/${ceph_commands[$i]// /_}_--format_json-pretty
        { timeout 120 oc -n "${namespace}" exec  "${HOSTNAME}"-helper -- bash -c "${ceph_commands[$i]} --connect-timeout=15"  >> "${ceph_command_output_file}"; } 2>/dev/null &
        { timeout 120 oc -n "${namespace}" exec  "${HOSTNAME}"-helper -- bash -c "${ceph_commands[$i]} --connect-timeout=15 --format json-pretty" >> "${ceph_command_json_output_file}"; } 2>/dev/null & 
    done
fi
wait

# Collecting output of ceph volume commands
for ((i = 0; i < ${#ceph_volume_commands[@]}; i++)); do
    for osd_pod in $(timeout 120 oc get pods -n "${namespace}" -l app=rook-ceph-osd --no-headers | grep -w "Running" | awk '{print $1}'); do
        while true; do
            if [ "$(jobs -rp | wc -l)" -lt "${cpu_numbers}" ]; then break; fi
            sleep 3
        done
        printf " -> Fetching output for '%s' from %s pod\n"  "${ceph_volume_commands[$i]}" "${osd_pod}"
        ceph_volume_command_output_file=${COMMAND_OUTPUT_DIR}/${ceph_volume_commands[$i]// /_}
        { timeout 120 oc -n "${namespace}" exec "${osd_pod}" -- bash -c "${ceph_volume_commands[$i]}" >> "${ceph_volume_command_output_file}"; } 2>/dev/null &
    done
done
wait

# Collecting ceph prepare volume logs
for node in $(timeout 120 oc get nodes -l cluster.ocs.openshift.io/openshift-storage='' --no-headers | grep -w 'Ready' | awk '{print $1}'); do
    while true; do
        if [ "$(jobs -rp | wc -l)" -lt "${cpu_numbers}" ]; then break; fi
        sleep 3
    done
    printf " -> Fetching prepare volume logs from node: %s \n"  "${node}"
    node_output_collection_path=${base_collection_path}/osd_prepare_volume_logs/${node}
    mkdir -p "${node_output_collection_path}"
    { timeout 120 oc debug  nodes/"${node}" -- bash -c "test -f /host/var/lib/rook/log/${namespace}/ceph-volume.log && cat /host/var/lib/rook/log/${namespace}/ceph-volume.log" > "${node_output_collection_path}"/ceph-volume.log; } 2>/dev/null &
done
wait
