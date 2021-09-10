# Build container to generate overrides for an rpm-ostree based system

Goal: support taking a set of RPMs *as well* as an arbitrary set of files in `rootfs/overlays`
 (which we turn into an RPM) and using this builder container, generate
 an output container that can be applied by the https://github.com/openshift/machine-config-operator/

Accepted inputs:

 - rootfs/overlay: Arbitrary filesystem tree.
 - rpms/overlay: Passed to `rpm-ostree install`
 - rpms/overrides: Passed to `rpm-ostree override replace`

Example: [Dockerfile.test](Dockerfile.test).




