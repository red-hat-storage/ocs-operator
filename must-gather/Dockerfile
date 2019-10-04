FROM quay.io/openshift/origin-must-gather:4.2

RUN sed -i 's/enabled=1/enabled=0/g' /etc/yum/pluginconf.d/subscription-manager.conf \
    && cat /etc/yum/pluginconf.d/subscription-manager.conf \
    && sed -i 's/enabled=1/enabled=0/g' /etc/yum/pluginconf.d/ovl.conf && cat /etc/yum/pluginconf.d/ovl.conf \
    && sed -i 's/enabled=1/enabled=0/g' /etc/yum/pluginconf.d/product-id.conf && cat /etc/yum/pluginconf.d/product-id.conf \
    && sed -i 's/enabled=1/enabled=0/g' /etc/yum/pluginconf.d/search-disabled-repos.conf && cat /etc/yum/pluginconf.d/search-disabled-repos.conf \
    && rm -rf /etc/yum.repos.d/*

COPY Centos-Base.repo  /etc/yum.repos.d/

RUN rpm --import 'https://download.ceph.com/keys/release.asc'

RUN yum install -y https://dl.fedoraproject.org/pub/epel/epel-release-latest-7.noarch.rpm

RUN su -c 'rpm -Uvh https://download.ceph.com/rpm-luminous/el7/noarch/ceph-release-1-0.el7.noarch.rpm'

RUN yum install -y \ 
    http://mirror.centos.org/centos/7/extras/x86_64/Packages/python-werkzeug-0.9.1-2.el7.noarch.rpm \
    http://mirror.centos.org/centos/7/extras/x86_64/Packages/python-itsdangerous-0.23-2.el7.noarch.rpm \
    http://mirror.centos.org/centos/7/os/x86_64/Packages/audit-libs-python-2.8.5-4.el7.x86_64.rpm \
    http://mirror.centos.org/centos/7/os/x86_64/Packages/cryptsetup-2.0.3-5.el7.x86_64.rpm \
    http://download-ib01.fedoraproject.org/pub/epel/7/x86_64/Packages/l/libbabeltrace-1.2.4-3.el7.x86_64.rpm \
    http://download-ib01.fedoraproject.org/pub/epel/7/x86_64/Packages/l/leveldb-1.12.0-11.el7.x86_64.rpm \
    http://mirror.centos.org/centos/7/extras/x86_64/Packages/python-flask-0.10.1-4.el7.noarch.rpm \
    http://mirror.centos.org/centos/7/os/x86_64/Packages/device-mapper-event-1.02.158-2.el7.x86_64.rpm \
    http://download-ib01.fedoraproject.org/pub/epel/7/x86_64/Packages/l/lttng-ust-2.4.1-4.el7.x86_64.rpm \
    ceph

# Save original gather script
RUN mv /usr/bin/gather /usr/bin/gather_original

# copy all collection scripts to /usr/bin
COPY collection-scripts/* /usr/bin/

RUN mkdir /templates
COPY templates /templates

ENTRYPOINT /usr/bin/gather
