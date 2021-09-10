# scratch container
FROM scratch

# copy rpms into scratch container
COPY rpms/ /rpms/

# So that podman create doesn't error out
ENTRYPOINT /noentry
