#!/bin/bash
	
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
echo "--- Noobaa ---"
oc get noobaa --all-namespaces -o yaml
