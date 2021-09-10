# Pre-requisite
Make sure that you have podman and buildah installed

# Building the container image
```
$ cd test-container
$ buildah bud -t rhcos-custom-content .
```

# Pushing the locally built image to a registry
For example: Pushing the image to quay.io
```
$ podman push localhost/rhcos-custom-content:latest quay.io/<username>/rhcos-custom-content:latest
```

# 