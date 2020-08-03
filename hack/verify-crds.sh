#!/bin/bash

FILES="./deploy/crds/*crd.yaml"
FAILS=false
for f in $FILES
do
    if [[ $(./_output/tools/bin/yq r $f apiVersion) == "apiextensions.k8s.io/v1beta1" ]]; then
        if [[ $(./_output/tools/bin/yq r $f spec.validation.openAPIV3Schema.properties.metadata.description) != "null" ]]; then
            echo "Error: cannot have a metadata description in $f"
            FAILS=true
        fi

        if [[ $(./_output/tools/bin/yq r $f spec.preserveUnknownFields) != "false" ]]; then
            echo "Error: pruning not enabled (spec.preserveUnknownFields != false) in $f"
            FAILS=true
        fi
    fi
done

if [ "$FAILS" = true ] ; then
    exit 1
fi

