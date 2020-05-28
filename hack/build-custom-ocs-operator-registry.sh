#!/bin/bash
set -e

SCRIPT_NAME="$(basename "$0")"
source hack/common.sh

UNSTAGED_COMMENT="[${SCRIPT_NAME}] My unstaged changes"
TMP_COMMENT="[${SCRIPT_NAME}] My new tmp commit"

function convert_to_grep_compatible_string() {
  echo "$1" |sed -e 's@\[@\\\[@g' -e 's@\]@\\\]@g'
}

function git_revert() {
  local GREP_TMP_COMMENT
  GREP_TMP_COMMENT="$(convert_to_grep_compatible_string "${TMP_COMMENT}")"
  local GREP_UNSTAGED_COMMENT
  GREP_UNSTAGED_COMMENT="$(convert_to_grep_compatible_string "${UNSTAGED_COMMENT}")"
  git log -1 |grep "${GREP_TMP_COMMENT}" && git reset --hard HEAD~1
  git log -1 |grep "${GREP_UNSTAGED_COMMENT}" && git reset --soft HEAD~1
  # git stash list |head -n1 |grep "${GREP_UNSTAGED_COMMENT}" && git stash pop
}

function help() {
  echo "Helps developer to create a custom OCS Operator Registry image and"
  echo "push it into any docker image registry platforms (like quay.io, dockerhub etc)"
  echo -e "\nPre-requisites"
  echo -e "\tUser has to 'docker login' to any of the docker registry platform"
  echo -e "\tUser has to create 'ocs-operator' and 'ocs-registry' repos in the registry platform"
  echo -e "\nArgumentns"
  echo -e "\t--dryrun  : " \
  "if set, could run the script in a 'mock' mode, where commands will be printed, without being executed"
  echo -e "\t--help    : " \
  "prints this help."
  echo -e "\nEnvironment Vars"
  echo -e "\tIMAGE_REGISTRY: which image registry to be used (quay.io or docker.io)"
  echo -e "\tREGISTRY_NAMESPACE: username which is used to login into above IMAGE_REGISTRY"
  echo -e "\tIMAGE_BUILD_CMD: command used to build the container images (docker or podman)"
  echo -e "\tIMAGE_TAG: a common tag, which describes your current changes," \
  "\n\t\t\tthis will be internally prepended with '<REGISTRY_NAMESPACE>-<COMPONENT>-<IMAGE_TAG>'," \
  "\n\t\t\twhere <COMPONENT> is 'ocs-operator' or 'ocs-registry'"
  echo -e "\nNote:"
  echo -e "\tAll other 'make' env variables can be re-used here"
  echo -e "\tFor more details on common variables, please see 'hack/common.sh' script\n"
  echo -e "\nEg: ROOK_IMAGE=rook/ceph:v1.3.4 IMAGE_REGISTRY=docker.io REGISTRY_NAMESPACE=dockHubUser1 IMAGE_TAG=with-independent-mode-and-latest-rook-changes-v22 IMAGE_BUILD_CMD=podman bash hack/${SCRIPT_NAME}"
  echo "Eg: ECHO_ONLY=true IMAGE_REGISTRY=quay.io REGISTRY_NAMESPACE=quayUser1 IMAGE_TAG=with-multus-changes-v3 IMAGE_BUILD_CMD=docker bash hack/${SCRIPT_NAME}"
}

function check_container_services() {
  local c_daemon=""
  local container_services=( docker podman )
  for c_daemon in "${container_services[@]}";do
    systemctl --no-pager -l status "${c_daemon}" 1>/dev/null 2>&1 && break
    c_daemon=""
  done
  [ -z "${c_daemon}" ] && return 1
  return 0
}

echo "$@" |grep -e "-h" 1>/dev/null 2>&1 && help && exit 0
echo "$@" |grep -e "--dryrun" 1>/dev/null 2>&1 && ECHO_ONLY="true"

[ -z "$GOPATH" ] && GOPATH="$(pwd |sed 's@/src/\?.*@@g')"
[ -z "$GOPATH" ] && GOPATH="$(go env GOPATH)"
[ -n "$ECHO_ONLY" ] && ECHO_ONLY="echo "

export GOPATH

if ! check_container_services;then
  echo "No container services (like docker or podman) are running on your system" >&2
  exit 1
fi

operatorTag="${REGISTRY_NAMESPACE}-ocs-operator-${IMAGE_TAG}"
registryTag="${REGISTRY_NAMESPACE}-ocs-registry-${IMAGE_TAG}"

# if IMAGE_TAG is *not* provided, put the same default tag into 'operatorTag' and 'registryTag'
[ "${IMAGE_TAG}" = "${DEFAULT_IMAGE_TAG}" ] && operatorTag="${IMAGE_TAG}" && registryTag="${IMAGE_TAG}"

operatorRepo="${IMAGE_REGISTRY}/${REGISTRY_NAMESPACE}/ocs-operator:${operatorTag}"
registryRepo="${IMAGE_REGISTRY}/${REGISTRY_NAMESPACE}/ocs-registry:${registryTag}"

# if there are any unstaged changes, take them into an unstaged commit
if ! git diff --exit-code >/dev/null;then
  ${ECHO_ONLY} git commit -a -s -m "${UNSTAGED_COMMENT}"
  # ${ECHO_ONLY} git stash push -m "${UNSTAGED_COMMENT}"
fi

# set a trap to revert things back
${ECHO_ONLY} trap git_revert 0 1 SIGHUP SIGINT SIGQUIT SIGTRAP SIGABRT SIGBUS SIGUSR1 SIGUSR2 SIGPIPE SIGTERM SIGSTOP

${ECHO_ONLY} make IMAGE_TAG="${operatorTag}" update-generated

${ECHO_ONLY} make IMAGE_TAG="${operatorTag}" gen-latest-csv
# need to run 'gen-latest-deploy-yaml' make target for 'ocs-operator-ci'
${ECHO_ONLY} make IMAGE_TAG="${registryTag}" gen-latest-deploy-yaml
[ -z "${ECHO_ONLY}" ] && git commit -a -s -m "${TMP_COMMENT}"
${ECHO_ONLY} make IMAGE_TAG="${operatorTag}" verify-latest-csv

${ECHO_ONLY} make IMAGE_TAG="${registryTag}" ocs-operator-ci

${ECHO_ONLY} make IMAGE_TAG="${operatorTag}" ocs-operator
${ECHO_ONLY} "${IMAGE_BUILD_CMD}" push "$operatorRepo"

${ECHO_ONLY} make IMAGE_TAG="${registryTag}" ocs-registry
${ECHO_ONLY} "${IMAGE_BUILD_CMD}" push "${registryRepo}"
