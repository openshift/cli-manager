FROM registry.ci.openshift.org/ocp/builder:rhel-8-golang-1.21-openshift-4.16 AS builder
WORKDIR /go/src/github.com/openshift/cli-manager
COPY . .
RUN make build --warn-undefined-variables

FROM registry.ci.openshift.org/ocp/4.16:base
COPY --from=builder /go/src/github.com/openshift/cli-manager/cli-manager /usr/bin/
COPY --from=builder /usr/bin/git /usr/bin/

LABEL io.k8s.display-name="OpenShift CLI Manager Command" \
      io.k8s.description="OpenShift is a platform for developing, building, and deploying containerized applications." \
      io.openshift.tags="openshift,cli-manager"
