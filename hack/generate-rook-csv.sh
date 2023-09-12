#!/usr/bin/env bash
set -e

source hack/ensure-operator-sdk.sh

#############
# VARIABLES #
#############

yq="${YQv3:-yq}"
PLATFORM=$(go env GOARCH)

YQ_CMD_DELETE=("$yq" delete -i)
YQ_CMD_MERGE_OVERWRITE=("$yq" merge --inplace --overwrite --prettyPrint)
YQ_CMD_MERGE=("$yq" merge --arrays=append --inplace)
YQ_CMD_WRITE=("$yq" write --inplace -P)
CSV_FILE_NAME="deploy/csv-templates/crds/rook/manifests/rook-ceph.clusterserviceversion.yaml"
CEPH_EXTERNAL_SCRIPT_FILE="https://raw.githubusercontent.com/rook/rook/master/deploy/examples/create-external-cluster-resources.py"
wget --no-verbose $CEPH_EXTERNAL_SCRIPT_FILE
ASSEMBLE_FILE_COMMON="deploy/csv-templates/rook_olm_files/metadata-common.yaml"
ASSEMBLE_FILE_OCP="deploy/csv-templates/rook_olm_files/metadata-ocp.yaml"
#############
# FUNCTIONS #
#############

function generate_csv() {
    # If you want to use specific branch, tags or commit has refer to this doc https://github.com/kubernetes-sigs/kustomize/blob/master/examples/remoteBuild.md#remote-directories
    kubectl kustomize https://github.com/rook/rook//deploy/examples?ref=release-1.13 | "${OPERATOR_SDK}" generate bundle --package="rook-ceph" --output-dir="deploy/csv-templates/crds/rook/" --extra-service-accounts=rook-ceph-system,rook-csi-rbd-provisioner-sa,rook-csi-rbd-plugin-sa,rook-csi-cephfs-provisioner-sa,rook-csi-nfs-provisioner-sa,rook-csi-nfs-plugin-sa,rook-csi-cephfs-plugin-sa,rook-ceph-system,rook-ceph-rgw,rook-ceph-purge-osd,rook-ceph-osd,rook-ceph-mgr,rook-ceph-cmd-reporter

    directory="deploy/csv-templates/crds/rook/manifests"
    filename=$(basename "$CSV_FILE_NAME")

    # Below line removes the ObjectBucket from owned CRD section and converts yaml to json file
    yq read --prettyPrint --tojson "$CSV_FILE_NAME" | jq 'del(.spec.customresourcedefinitions.owned[] | select(.kind | test("ObjectBucket.*")))' >"$directory"/temp.json
    # Belo lines convert the json to yaml
    yq read --prettyPrint "$directory"/temp.json >"$CSV_FILE_NAME"
    # Below lines remove the extra json file
    rm -f "$directory"/temp.json

    for file in "$directory"/*; do
        if [ -f "$file" ]; then
            filename=$(basename "$file")
            # We want to remove the description from rook-crd files only
            if [[ "$filename" == "ceph.rook.io"* ]]; then
                # Below lines converts the file to json format and then remove the description field from the file.
                yq read --prettyPrint --tojson "$file" | jq 'walk(if type == "object" then with_entries(select(.key != "description")) else . end)' >"$directory"/temp.json
                # Belo lines convert the json to yaml
                yq read --prettyPrint "$directory"/temp.json >"$directory"/"$filename"
                # Below lines remove the extra json file
                rm -f "$directory/temp.json"
            fi
        fi
    done

    # cleanup to get the expected state before merging the real data from assembles
    "${YQ_CMD_DELETE[@]}" "$CSV_FILE_NAME" 'spec.installModes[*]'
    "${YQ_CMD_DELETE[@]}" "$CSV_FILE_NAME" 'spec.keywords[0]'
    "${YQ_CMD_DELETE[@]}" "$CSV_FILE_NAME" 'spec.maintainers[0]'

    "${YQ_CMD_MERGE_OVERWRITE[@]}" "$CSV_FILE_NAME" <(cat $ASSEMBLE_FILE_COMMON)
    "${YQ_CMD_WRITE[@]}" "$CSV_FILE_NAME" metadata.annotations.externalClusterScript "$(base64 <create-external-cluster-resources.py)"
    "${YQ_CMD_WRITE[@]}" "$CSV_FILE_NAME" metadata.name "rook-ceph.v${VERSION}"

    "${YQ_CMD_MERGE[@]}" "$CSV_FILE_NAME" <(cat $ASSEMBLE_FILE_OCP)

    # We don't need to include these files in csv as ocs-operator creates its own.
    rm -rf "deploy/csv-templates/crds/rook/manifests/rook-ceph-operator-config_v1_configmap.yaml"
    rm -rf "deploy/csv-templates/crds/rook/manifests/objectbucket.io_objectbuckets.yaml"
    rm -rf "deploy/csv-templates/crds/rook/manifests/objectbucket.io_objectbucketclaims.yaml"
    rm -rf "deploy/csv-templates/crds/rook/metadata/annotation.yaml"
    rm -rf bundle.Dockerfile
    rm -rf create-external-cluster-resources.py

    # This change are just to make the CSV file as it was earlier and as ocs-operator reads.
    # Skipping this change for darwin since `sed -i` doesn't work with darwin properly.
    # and the csv is not ever needed in the mac builds.
    if [[ "$OSTYPE" != "darwin"* ]]; then
        sed -i 's/image: rook\/ceph:.*/image: {{.RookOperatorImage}}/g' "$CSV_FILE_NAME"
        sed -i 's/name: rook-ceph.v.*/name: rook-ceph.v{{.RookOperatorCsvVersion}}/g' "$CSV_FILE_NAME"
        sed -i 's/version: 0.0.0/version: {{.RookOperatorCsvVersion}}/g' "$CSV_FILE_NAME"
    fi

    mv "$CSV_FILE_NAME" "deploy/csv-templates/"
    mv "deploy/csv-templates/rook-ceph.clusterserviceversion.yaml" "deploy/csv-templates/rook-csv.yaml.in"
    mv "deploy/csv-templates/crds/rook/manifests/"* "deploy/csv-templates/crds/rook"
    rm -rf "deploy/csv-templates/crds/rook/manifests"
    rm -rf "deploy/csv-templates/crds/rook/metadata"
}

if [ "$PLATFORM" == "amd64" ]; then
    generate_csv
fi
