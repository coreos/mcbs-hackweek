# Introduction
Test container built is available to pull from quay.io/skumari/rhcos-custom-content:latest

# Extract or mount the container image locally
## Using oc
```
$ mkdir /tmp/my-container/
$ oc image extract --registry-config ~/.docker/config.json --path /:/tmp/my-container/ quay.io/skumari/rhcos-custom-content:latest
```

## Using podman

```
# imgid=`podman pull -q quay.io/skumari/rhcos-custom-content:latest`
# cid=`podman create --net=none --name rhcos-payload-container $imgid`
# mnt_path=`podman mount $cid`
# ls -lR $mnt_path/rpms

# ls -lR $mnt_path/rpms
/var/lib/containers/storage/overlay/7f6c033935999f38f16bab678924776157b083f2611b38954e017f6e992111e0/merged/rpms:
total 0
drwxrwxr-x. 1 root root 184 Sep 10 10:56 install
drwxrwxr-x. 1 root root 312 Sep 10 10:57 overrides

/var/lib/containers/storage/overlay/7f6c033935999f38f16bab678924776157b083f2611b38954e017f6e992111e0/merged/rpms/install:
total 1544
-rw-rw-r--. 1 root root 114580 May 20  2020 libqb-1.0.3-12.el8.x86_64.rpm
-rw-rw-r--. 1 root root 912320 May 29  2020 protobuf-3.5.0-13.el8.x86_64.rpm
-rw-rw-r--. 1 root root 549692 Mar 18 16:28 usbguard-1.0.0-2.el8.x86_64.rpm

/var/lib/containers/storage/overlay/7f6c033935999f38f16bab678924776157b083f2611b38954e017f6e992111e0/merged/rpms/overrides:
total 83996
-rw-rw-r--. 1 root root  7291764 Sep  2 23:20 kernel-4.18.0-340.el8.x86_64.rpm
-rw-rw-r--. 1 root root 39385436 Sep  2 23:22 kernel-core-4.18.0-340.el8.x86_64.rpm
-rw-rw-r--. 1 root root 31223904 Sep  2 23:22 kernel-modules-4.18.0-340.el8.x86_64.rpm
-rw-rw-r--. 1 root root  8100844 Sep  2 22:41 kernel-modules-extra-4.18.0-340.el8.x86_64.rpm

```

Once done, unmount the container
```
# podman unmount $cid
```






