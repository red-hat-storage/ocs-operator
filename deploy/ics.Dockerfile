FROM scratch

LABEL operators.operatorframework.io.bundle.mediatype.v1=registry+v1
LABEL operators.operatorframework.io.bundle.manifests.v1=manifests/
LABEL operators.operatorframework.io.bundle.metadata.v1=metadata/
LABEL operators.operatorframework.io.bundle.package.v1=ocs-operator
LABEL operators.operatorframework.io.bundle.channels.v1=alpha
LABEL operators.operatorframework.io.bundle.channel.default.v1=alpha

COPY ics-operator/manifests /manifests/
COPY ics-operator/metadata/annotations.yaml /metadata/annotations.yaml

# These are labels needed to control how the pipeline should handle this container image
# This first label tells the pipeline that this is a bundle image and should be
# delivered via an index image
LABEL com.redhat.delivery.operator.bundle=true
# This second label tells the pipeline which versions of OpenShift the operator supports.
# This is used to control which index images should include this operator.
LABEL com.redhat.openshift.versions="v4.12"
