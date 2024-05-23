#!/usr/bin/env bash

# shellcheck disable=SC2034
# don't fail on unused variables - this is for sourcing


# Align with installation instructions from:
# https://sdk.operatorframework.io/docs/installation/
case "$(uname -m)" in
	x86_64)
		OPERATOR_SDK_ARCH="amd64"
		;;
	aarch64)
		OPERATOR_SDK_ARCH="arm64"
		;;
	*)
		OPERATOR_SDK_ARCH="$(uname -m)"
		;;
esac
OPERATOR_SDK_OS=$(uname | awk '{print tolower($0)}')

OPERATOR_SDK_URL="${OPERATOR_SDK_URL:-https://github.com/operator-framework/operator-sdk/releases/download}"
OPERATOR_SDK_VERSION="${OPERATOR_SDK_VERSION:-v1.25.4}"
OPERATOR_SDK_DL_URL_FULL="${OPERATOR_SDK_URL}/${OPERATOR_SDK_VERSION}/operator-sdk_${OPERATOR_SDK_OS}_${OPERATOR_SDK_ARCH}"
OPERATOR_SDK="${OUTDIR_TOOLS}/operator-sdk-${OPERATOR_SDK_VERSION}"
