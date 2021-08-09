#!/bin/bash

set -eEuo pipefail

function help_text () {
  cat <<EOF
DESCRIPTION
  Generate a "midstream" bundle or appregistry for OCS/ODF. The
  bundle/appregistry is used in upstream testing of downstream forks of
  OCS/ODF applications.
  For example, generate a bundle/appregistry using openly-available
  https://quay.io/organization/ocs-dev "release-4.9" images for use in
  https://github.com/red-hat-storage/ocs-ci CI testing.

USAGE
  ${0} <bundle|appregistry> <target ODF version>
    target ODF version :  "master" or "latest-4.9"
EOF
}

#
# ARGUMENTS
#
if [[ $# -ne 2 ]]; then
  help_text
  exit 1
fi

BUNDLE_OR_APPREGISTRY=$1

TARGET_ODF_VERSION="$2"


#
# FUNCTIONS / DEFINITIONS
#

function latest_dockerhub_image_matching_filter () {
  local repo="$1"
  local tag_filter="${2:-.}" # match anything by default
  URL="https://registry.hub.docker.com/v2/repositories/${repo}/tags/"
  tag="$(curl -s -S "$URL" | jq --raw-output '.results[].name' | grep "${tag_filter}" | sort --numeric-sort --reverse | head -n1)"
  echo "${repo}:${tag}"
}

# return the image:tag referenced by sha
function dockerhub_image_with_sha () {
  local repo="$1"
  local tag="$2"
  URL="https://registry.hub.docker.com/v2/repositories/${repo}/tags/"
  # complex jq query: find all images with the tag, there should only be one "active" image for any
  # tag, from the images of the tag chose the linux/amd64 one, and print it's sha
  sha="$(curl -s -S "$URL" | jq --raw-output ".results[] | select( .name == \"${tag}\" and .tag_status == \"active\" ) | .images[] | select( .architecture == \"amd64\" and .os == \"linux\" ) | .digest")"
  echo "${repo}@${sha}"
}

# pick the latest image by sorted version number matching a grep filter
function latest_quay_image_matching_filter () {
  local repo="$1"
  local tag_filter="${2:-.}" # match anything by default
  URL="https://quay.io/api/v1/repository/${repo}/tag/"
  tag="$(curl -s -S "$URL" | jq --raw-output '.tags[].name' | grep "${tag_filter}" | sort --numeric-sort --reverse | head -n1)"
  echo "quay.io/${repo}:${tag}"
}

# given a generic tag like 'latest-4.9', find a more specific version of the image
# if a more specific tag doesn't exist, return an image referenced by sha256
function specific_quay_image () {
  local repo="$1"
  local generic_tag="$2" # a generic image tag like 'latest-4.9'
  BASEURL="https://quay.io/api/v1/repository/${repo}"
  # get the image ID for the tag (the first image in the list has the right ID)
  id="$(curl -s -S "$BASEURL/tag/${generic_tag}/images" | jq --raw-output '.images[0].id')"
  # list all images with the ID found except images named the generic tag given
  images="$(curl -s -S "$BASEURL/tag/" | jq --raw-output ".tags[] | select( .image_id == \"$id\" and .name != \"${generic_tag}\" ) | .name")"
  if [[ -z "${images}" ]]; then
    # Only the generic tag exists. It's possible a CI run might pull a different version than we
    # process here if we just use a  generic tag. the best we can do is to get the sha of the image,
    # which isn't human-friendly but is at least guaranteed to be a specific image.
    # complext jq query: find all images with the tag, make them an array, sort the array by their
    # created timestamp, then pick the latest one (at the end of the list), and return its sha
    sha="$(curl -s -S "$BASEURL/tag/" | jq --raw-output "[ .tags[] | select( .name == \"${generic_tag}\" ) ] | sort_by( .start_ts ) | .[-1].manifest_digest")"
    echo "quay.io/${repo}@${sha}"
    return
  fi
  # assume the highest version image is the one to pick if there are multiple
  # also chooses vX.Y.Z over vX.Y, and vX.Y over vX (more specificity is better)
  tag="$(echo "${images}" | sort --numeric-sort --reverse | head -n1)"
  echo "quay.io/${repo}:${tag}"
}

# upstream masters of OCS/ODF components
function replace_versions_master () {
  CEPH_PATTERN="ceph/daemon-base:.*$"
  # the ceph/daemon-base:latest-master image gets replaced in nightly builds, and hese images don't
  # have more specific tags created, so the best we can do is to get the sha of the image
  CEPH_IMAGE="$(specific_quay_image 'ceph/daemon-base' 'latest-master')"
  sed -i "s%image: ${CEPH_PATTERN}%image: ${CEPH_IMAGE}%" "$CSV_FILE"

  NOOBAA_CORE_PATTERN="noobaa/noobaa-core:.*$"
  NOOBAA_CORE_IMAGE="$(latest_dockerhub_image_matching_filter 'noobaa/noobaa-core' 'master')"
  sed -i "s%image: ${NOOBAA_CORE_PATTERN}%image: ${NOOBAA_CORE_IMAGE}%" "$CSV_FILE"

  NOOBAA_OPERATOR_PATTERN="noobaa/noobaa-operator:.*$"
  NOOBAA_OPERATOR_IMAGE="$(latest_dockerhub_image_matching_filter 'noobaa/noobaa-operator' 'master')"
  sed -i "s%image: ${NOOBAA_OPERATOR_PATTERN}%image: ${NOOBAA_OPERATOR_IMAGE}%" "$CSV_FILE"

  ROOK_PATTERN="rook/ceph:.*$"
  # It's possible but challenging to cross-reference the rook/ceph:master image with a numbered
  # release. It's 'good enough' to just reference the rook/ceph:master image by sha like we do the
  # ceph master image.
  ROOK_IMAGE="$(dockerhub_image_with_sha 'rook/ceph' 'master')"
  sed -i "s%image: ${ROOK_PATTERN}%image: ${ROOK_IMAGE}%" "${CSV_FILE}"
}

# v4.9+
function replace_versions_odf () {
  local odf_version_tag="$1" # e.g., latest-4.9
  local ceph_target="$2" # e.g., v16.2

  # Use the latest stable Ceph image we can find. Don't use specific_quay_image for ceph/ceph the
  # repo uses image manifests which are a HUGE pain to process from quay's bad api.
  CEPH_PATTERN="ceph/daemon-base:.*$"
  CEPH_IMAGE=$(latest_quay_image_matching_filter 'ceph/ceph' "${ceph_target}")
  sed -i "s%image: ${CEPH_PATTERN}%image: ${CEPH_IMAGE}%" "$CSV_FILE"

  # TODO: update with nooba quay.io/release-4.9 when available (copied from replace_versions_master)
  NOOBAA_CORE_PATTERN="noobaa/noobaa-core:.*$"
  NOOBAA_CORE_IMAGE="$(latest_dockerhub_image_matching_filter 'noobaa/noobaa-core' 'master')"
  sed -i "s%image: ${NOOBAA_CORE_PATTERN}%image: ${NOOBAA_CORE_IMAGE}%" "$CSV_FILE"

  # TODO: update with nooba quay.io/release-4.9 when available (copied from replace_versions_master)
  NOOBAA_OPERATOR_PATTERN="noobaa/noobaa-operator:.*$"
  NOOBAA_OPERATOR_IMAGE="$(latest_dockerhub_image_matching_filter 'noobaa/noobaa-operator' 'master')"
  sed -i "s%image: ${NOOBAA_OPERATOR_PATTERN}%image: ${NOOBAA_OPERATOR_IMAGE}%" "$CSV_FILE"

  ROOK_PATTERN="rook/ceph:.*$"
  ROOK_IMAGE="$(specific_quay_image 'ocs-dev/rook-ceph' "${odf_version_tag}")"
  sed -i "s%image: ${ROOK_PATTERN}%image: ${ROOK_IMAGE}%" "${CSV_FILE}"
}


#
# MAIN
#

# populates a default CSV_VERSION or accepts an override from the local env
source hack/common.sh

case $TARGET_ODF_VERSION in
  master)
    # take default CSV_VERSION from hack/common.sh
    ;;
  latest-4.9)
    export CSV_VERSION=4.9.0
    ;;
  *)
    echo "Unknown target version: $TARGET_ODF_VERSION"
    help_text
    exit 1
    ;;
esac

case $BUNDLE_OR_APPREGISTRY in
  bundle)
    CSV_FILE=/manifests/ocs-operator.clusterserviceversion.yaml
    ;;
  appregistry)
    # generate the appregistry which we will modify here
    source hack/generate-appregistry.sh

    CSV_FILE=build/_output/appregistry/olm-catalog/ocs-operator/${CSV_VERSION}/ocs-operator.v${CSV_VERSION}.clusterserviceversion.yaml
    ;;
  *)
    echo "Unknown bundle/appregistry selection: $BUNDLE_OR_APPREGISTRY"
    help_text
    exit 1
    ;;
esac

case $TARGET_ODF_VERSION in
  master)
    replace_versions_master
    ;;
  latest-4.9)
    replace_versions_odf 'latest-4.9' 'v16.2'
    ;;
  *)
    echo "Unknown target version: $TARGET_ODF_VERSION"
    help_text
    exit 1
    ;;
esac
