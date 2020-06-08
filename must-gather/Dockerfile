FROM quay.io/multi-arch/openshift4-ose-cli:v4.3.2-202002122007-ppc64le

# copy all collection scripts to /usr/bin
COPY collection-scripts/* /usr/bin/

RUN mkdir /templates
COPY templates /templates

ENTRYPOINT /usr/bin/gather
