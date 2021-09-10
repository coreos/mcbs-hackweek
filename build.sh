#!/bin/bash
set -xeuo pipefail
yum -y install rpm-build && yum clean all
mv /root/override-builder.sh /usr/bin/coreos-override-builder
