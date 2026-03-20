ARG BASE_IMAGE=docker.io/apache/nifi:2.0.0
FROM ${BASE_IMAGE}

USER root

# The upstream image keeps key runtime paths readable only to uid/gid 1000.
# OpenShift restricted SCC assigns a random uid, so the proof mirror relaxes
# only the existing chart runtime paths that must be executable/readable.
RUN chmod 0755 /opt/nifi/nifi-current/bin/nifi.sh /opt/nifi/nifi-current/bin/nifi-env.sh \
 && chmod -R a+rX /opt/nifi/nifi-current/lib

USER nifi
