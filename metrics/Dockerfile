FROM quay.io/ceph/ceph:v17

ADD ./metrics-exporter /

ENTRYPOINT ["/metrics-exporter"]

EXPOSE 8080 8081
