FROM registry.fedoraproject.org/fedora:34
ADD build.sh override-builder.sh /root
RUN /root/build.sh && rm -f /root/build.sh
