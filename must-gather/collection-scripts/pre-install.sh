#!/bin/bash

ns="openshift-storage"

POD_TEMPLATE="/templates/pod.template"

SED_DELIMITER=$(echo -en "\001");
safe_replace () {
    sed "s${SED_DELIMITER}${1}${SED_DELIMITER}${2}${SED_DELIMITER}g"
}

apply_helper_pod() {
    < ${POD_TEMPLATE} safe_replace "NAMESPACE" "$1" | safe_replace "IMAGE_NAME" "$2" | safe_replace "MUST_GATHER" "$HOSTNAME" > pod_helper.yaml
    oc apply -f pod_helper.yaml
}

# Add Ready nodes to the list
nodes=$(oc get nodes -l cluster.ocs.openshift.io/openshift-storage='' --no-headers | awk '/\yReady\y/{print $1}')

# storing storagecluster name
storageClusterPresent=$(oc get storagecluster -n openshift-storage -o go-template='{{range .items}}{{.metadata.name}}{{"\n"}}{{end}}')
deploy(){
     operatorImage=$(oc get pods -l app=rook-ceph-operator -n openshift-storage -o jsonpath="{range .items[*]}{@.spec.containers[0].image}+{end}" | tr "+" "\n" | head -n1)
     if [ -z "${storageClusterPresent}" ]; then
        echo "not creating helper pod since storagecluster is not present" | tee -a  "${BASE_COLLECTION_PATH}"/gather-debug.log
     elif [ "${operatorImage}" = "" ]; then
        echo "not able to find the rook's operator image. Skipping collection of ceph command output" | tee -a  "${BASE_COLLECTION_PATH}"/gather-debug.log
     else
          echo "creating helper pod" | tee -a  "${BASE_COLLECTION_PATH}"/gather-debug.log
          apply_helper_pod "$ns" "$operatorImage"
     fi

     for node in ${nodes}; do
          oc debug nodes/"${node}" --to-namespace="${ns}" -- bash -c "sleep 100m" &
          printf "debugging node %s \n" "${node}"
     done
}

labels(){
    if [ -n "${storageClusterPresent}" ]; then
     oc label pod -n openshift-storage "${HOSTNAME}"-helper must-gather-helper-pod=''
    fi
}

check_for_debug_pod(){
    debug_pod_name=$(oc get pods -n openshift-storage | grep "${node//./}-debug" | awk '{print $1}')
    # sleep for 60 seconds giving time for debug pod to get created
    sleep 60
    oc wait -n openshift-storage --for=condition=Ready pod/"$debug_pod_name" --timeout=200s
    if [ "$(oc get pods -n openshift-storage | grep "${node//./}-debug" | awk '{print $2}')" == "1/1" ] ; then
        oc label -n openshift-storage pod "$debug_pod_name" "${node//./}"-debug='ready'
    fi
}

check_for_helper_pod(){
    # sleep for 60 seconds giving time for helper pod to get created
    sleep 60
    oc wait -n openshift-storage --for=condition=Ready pod/"${HOSTNAME}"-helper --timeout=200s
}

cleanup() {
  echo "checking for existing must-gather resource" | tee -a "${BASE_COLLECTION_PATH}"/gather-debug.log
  pods=$(oc get pods --no-headers -n openshift-storage -l must-gather-helper-pod='' | awk '{print $1}')
  if [ -n "${storageClusterPresent}" ] && [ -n "${pods}" ]; then
    SAVEIFS=$IFS # Save current IFS
    IFS=$'\n'    # Change IFS to new line
    pods=("$pods") # split to array $pods
    IFS=$SAVEIFS # Restore IFS
    echo "deleting existing must-gather resource" | tee -a "${BASE_COLLECTION_PATH}"/gather-debug.log
    for pod in "${pods[@]}"; do
      oc delete pod "${pod}" -n "${ns}"
    done
  fi
}

cleanup
deploy
labels
pids=()
if [ -n "${storageClusterPresent}" ]; then
    check_for_helper_pod &
    pids+=($!)
fi
for node in ${nodes}; do
    check_for_debug_pod &
    pids+=($!)
done

# wait for all pi ds
if [ -n "${pids[*]}" ]; then
    echo "waiting for ${pids[*]} to terminate" | tee -a  "${BASE_COLLECTION_PATH}"/gather-debug.log
    wait "${pids[@]}"
fi
