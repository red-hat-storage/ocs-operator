FROM quay.io/openshift/origin-cli:latest

ARG FROM_DIRECTORY=ocs-operator/must-gather
# copy all collection scripts to /usr/bin
COPY ${FROM_DIRECTORY}/collection-scripts/* /usr/bin/

RUN mkdir /templates
COPY ${FROM_DIRECTORY}/templates /templates

ENTRYPOINT /usr/bin/gather
