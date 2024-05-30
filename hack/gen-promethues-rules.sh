#!/usr/bin/env bash

MIXIN_PATH="metrics/mixin"
BUILD_PATH="metrics/mixin/build"
RULES_PATH="metrics/deploy"

#Create intermediate yamls to test if alerts are compiling a
echo "Creating Prometheus Rules And Alerts"

(cd $MIXIN_PATH && make prometheus_alert_rules.yaml prometheus_alert_rules_external.yaml)

echo "Running lint on generated Rules and Alerts"

(cd $MIXIN_PATH && make lint)

#Clean intermediate yamls
rm $MIXIN_PATH/prometheus_alert_rules.yaml $MIXIN_PATH/prometheus_alert_rules_external.yaml $MIXIN_PATH/prometheus_rules.yaml

set -e
set -x
# only exit with zero if all commands of the pipeline exit successfully
set -o pipefail

#Create build yamls temporary directory
# Make sure to start with a clean 'manifests' dir
rm -rf $BUILD_PATH/manifests
mkdir $BUILD_PATH/manifests

#Generate final Prometheus Rule Yamls
echo "Creating Deploy Manifest Prometheus Rules"

jsonnet -J vendor -m $BUILD_PATH/manifests "$BUILD_PATH/internal.jsonnet" | xargs -I{} sh -c 'cat {} | gojsontoyaml > {}.yaml; rm -f {}' -- {}
jsonnet -J vendor -m $BUILD_PATH/manifests "$BUILD_PATH/external.jsonnet" | xargs -I{} sh -c 'cat {} | gojsontoyaml > {}.yaml; rm -f {}' -- {}

# Rename and move the output rule yaml files
echo "Moving Deploy Manifest Prometheus Rules to $RULES_PATH"

mv $BUILD_PATH/manifests/prometheus-rules.yaml $RULES_PATH/prometheus-ocs-rules.yaml
mv $BUILD_PATH/manifests/prometheus-rules-external.yaml $RULES_PATH/prometheus-ocs-rules-external.yaml

#Clean build yamls temporary directory
rm -rf $BUILD_PATH/manifests
