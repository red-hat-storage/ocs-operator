#!/bin/bash

ns="openshift-storage"

POD_TEMPLATE_LATEST="/templates/pod.template.latest"
POD_TEMPLATE_STANDARD="/templates/pod.template.standard"

SED_DELIMITER=$(echo -en "\001");
safe_replace () {
    sed "s${SED_DELIMITER}${1}${SED_DELIMITER}${2}${SED_DELIMITER}g"
}

apply_standard_helper_pod() {
    < ${POD_TEMPLATE_STANDARD} safe_replace "NAMESPACE" "$1" | safe_replace "IMAGE_NAME" "$2" | safe_replace "MUST_GATHER" "$HOSTNAME" > pod_helper.yaml
    oc apply -f pod_helper.yaml
}

apply_latest_helper_pod() {
    < ${POD_TEMPLATE_LATEST} safe_replace "NAMESPACE" "$1" | safe_replace "IMAGE_NAME" "$2" | safe_replace "MUST_GATHER" "$HOSTNAME" > pod_helper.yaml
    oc apply -f pod_helper.yaml
}

# Add Ready nodes to the list
nodes=$(oc get nodes -l cluster.ocs.openshift.io/openshift-storage='' --no-headers | awk '/\yReady\y/{print $1}')

deploy(){
     operatorImage=$(oc get pods -l app=rook-ceph-operator -n openshift-storage -o jsonpath="{range .items[*]}{@.spec.containers[0].image}+{end}" | tr "+" "\n" | head -n1)
     if [ "${operatorImage}" = "" ]; then
          echo "not able to find the rook's operator image. Skipping collection of ceph command output" | tee -a  "${BASE_COLLECTION_PATH}"/gather-debug.log
     else
          current_version=$(oc get csv -n "${ns}" --no-headers | awk '{print $5}' | tr -dc '0-9')
          if [[ $current_version -ge 460 ]]; then
              apply_latest_helper_pod "$ns" "$operatorImage"
          else
              apply_standard_helper_pod "$ns" "$operatorImage"
          fi
     fi

     for node in ${nodes}; do
          oc debug nodes/"${node}" --to-namespace="${ns}" -- bash -c "sleep 100m" &
          printf "debugging node %s \n" "${node}"
     done
}

labels(){
     oc label pod -n openshift-storage "${HOSTNAME}"-helper must-gather-helper-pod=''
}

check_for_debug_pod(){
    for node in ${nodes}; do
            check_debug_pod=flase
            if [ "$(oc get pods -n openshift-storage | grep "${node//./}-debug" | awk '{print $2}')" != "1/1" ] ; then
                echo "waiting for the ${node//./}-debug pod to be in ready state"
                break
            else
                pod_name=$(oc get pods -n openshift-storage | grep "${node//./}-debug" | awk '{print $1}')
                oc label pod "$pod_name" "${node//./}"-debug='ready'
                check_debug_pod=true
            fi
    done
}

check_for_helper_pod(){
    check_helper_pod=flase
    if [ "$(oc get pods -n openshift-storage -l  must-gather-helper-pod='' | awk '{print $2}')" == "1/1" ] ; then
        check_helper_pod=true
    fi
}

deploy
labels
for i in {0..300..3}; do
    check_for_debug_pod
    check_for_helper_pod
    if [ $check_helper_pod = true ] && [ $check_debug_pod = true ] ; then
        echo "helper pod and debug pod are ready"
        break
    else 
       sleep 3
       echo "waiting for helper pod and debug pod for $i seconds"
    fi
done
