FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.22 as builder
WORKDIR /go/src/github.com/openshift/cli-manager
COPY . .
RUN make build --warn-undefined-variables

FROM registry.redhat.io/rhel9-2-els/rhel:9.2-1222
COPY --from=builder /go/src/github.com/openshift/cli-manager/cli-manager /usr/bin/
COPY --from=builder /usr/bin/git /usr/bin/git
RUN mkdir /licenses
COPY --from=builder /go/src/github.com/openshift/cli-manager/LICENSE /licenses/.

LABEL io.k8s.display-name="CLI Manager Command" \
      io.k8s.description="OpenShift is a platform for developing, building, and deploying containerized applications." \
      io.openshift.tags="openshift,cli-manager"
USER 1001

