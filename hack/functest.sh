#!/bin/bash

source hack/common.sh

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

