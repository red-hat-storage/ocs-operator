#!/bin/bash

source hack/common.sh

# This env-var is required for the functional tests to actually execute.
# This logic was added to the functional tests to prevent them from running
# when the unit tests are executed since both require files with the *_test.go postfix
export OCS_EXECUTE_FUNC_TESTS=1
echo "Running Functional Test Suite"
$OUTDIR_BIN/functests
if [ $? -ne 0 ]; then
	echo "dumping debug information"
	echo "--- PODS ----"
	oc get pods -n openshift-storage
	echo "---- PVs ----"
	oc get pv
	echo "--- StorageClasses ----"
	oc get storageclass --all-namespaces
	echo "--- StorageCluster ---"
	oc get storagecluster --all-namespaces -o yaml
	echo "--- CephCluster ---"
	oc get cephcluster --all-namespaces -o yaml
	echo "ERROR: Functest failed."
	exit 1
fi

