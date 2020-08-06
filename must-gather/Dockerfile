FROM quay.io/openshift/origin-cli:latest

# copy all collection scripts to /usr/bin
COPY collection-scripts /usr/bin/

RUN mkdir -p  /templates
COPY templates /templates

ENTRYPOINT /usr/bin/gather
