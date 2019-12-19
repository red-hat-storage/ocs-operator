#!/bin/bash
# https://github.com/red-hat-storage/ocs-ci/blob/master/docs/getting_started.md
set -e

source hack/common.sh 

ORIG_DIR=$(pwd)

mkdir -p $OUTDIR_OCS_CI
cd $OUTDIR_OCS_CI


# detect latest master hash of ocs-ci if we're not pinned to a specific commit hash
if [ -z "$REDHAT_OCS_CI_HASH" ]; then
	echo "Using latest ocs-ci master branch commit"
	REDHAT_OCS_CI_HASH=$(git ls-remote "$REDHAT_OCS_CI_REPO" | grep refs/heads/master | cut -f 1)
fi

echo "ocs-ci commit hash: $REDHAT_OCS_CI_HASH at repo: $REDHAT_OCS_CI_REPO"

DOWNLOAD_SRC=""
if ! [ -d "ocs-ci" ]; then
	DOWNLOAD_SRC="true"
elif ! [ "$(cat ocs-ci/git-hash)" = "$REDHAT_OCS_CI_HASH" ]; then
	rm -rf ocs-ci
	DOWNLOAD_SRC="true"
fi

if [ -n "${DOWNLOAD_SRC}" ]; then
	echo "Cloning code from $REDHAT_OCS_CI_REPO using hash $REDHAT_OCS_CI_HASH"
        # shellcheck disable=SC2086
	curl -L ${REDHAT_OCS_CI_REPO}/archive/${REDHAT_OCS_CI_HASH}/ocs-ci.tar.gz | tar xz ocs-ci-${REDHAT_OCS_CI_HASH}
	mv ocs-ci-"${REDHAT_OCS_CI_HASH}" ocs-ci
else
	echo "Using cached ocs-ci src"
fi

cd ocs-ci

oc patch ocsinitialization ocsinit -n openshift-storage --type json --patch  '[{ "op": "replace", "path": "/spec/enableCephTools", "value": true }]'
sleep 10

# record the hash in a file so we don't redownload the source if nothing changed.
echo "$REDHAT_OCS_CI_HASH" > git-hash

# we are faking an openshift-install cluster directory here
# for the ocs-ci test suite. All we need is to provide
# the auth credentials in the predictable directory structure
mkdir -p fakecluster/auth
cp "$KUBECONFIG" fakecluster/auth/kubeconfig

# Openshift CI runs this test within a pod with a randomized uid
# ocs-ci expects this randomized user to exist in the /etc/passwd file
# so we need to dynamically create an entry for the user
if [ -n "$OPENSHIFT_BUILD_NAMESPACE" ]; then
	echo "${USER_NAME:-default}:x:$(id -u):0:${USER_NAME:-default} user:${HOME}:/sbin/nologin" >> /etc/passwd
fi

# Create a Python virtual environment for the tests to execute with.
echo "Using $REDHAT_OCS_CI_PYTHON_BINARY"
$REDHAT_OCS_CI_PYTHON_BINARY -m venv .venv
source .venv/bin/activate
pip install --upgrade pip
pip install -r requirements.txt

# This is the test config we pass into ocs-ci's run-ci tool
cat << EOF > my-config.yaml
---
RUN:
  log_dir: "/tmp"
  kubeconfig_location: 'auth/kubeconfig' # relative from cluster_dir
  bin_dir: './bin'

DEPLOYMENT:
  force_download_installer: False
  force_download_client: False

ENV_DATA:
  cluster_name: null
  storage_cluster_name: 'test-storagecluster'
  storage_device_sets_name: "example-deviceset"
  cluster_namespace: 'openshift-storage'
  skip_ocp_deployment: true
  skip_ocs_deployment: true
EOF

# we want to handle errors explicilty at this point in order to dump debug info
set +e

echo "Running ocs-ci testsuite using -k $REDHAT_OCS_CI_TEST_EXPRESSION"
run-ci -k "$REDHAT_OCS_CI_TEST_EXPRESSION" --cluster-path "$(pwd)/fakecluster/" --ocsci-conf my-config.yaml

if [ $? -ne 0 ]; then
	"${ORIG_DIR}"/hack/dump-debug-info.sh
	echo "ERROR: red-hat-storage/ocs-ci test suite failed."
	exit 1
fi
