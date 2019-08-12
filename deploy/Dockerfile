FROM quay.io/openshift/origin-operator-registry

COPY olm-catalog /registry/ocs-catalog

# Initialize the database
RUN initializer --manifests /registry/ocs-catalog --output bundles.db

# There are multiple binaries in the origin-operator-registry
# We want the registry-server
ENTRYPOINT ["registry-server"]
CMD ["--database", "bundles.db"]
