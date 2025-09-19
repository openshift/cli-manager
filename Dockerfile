FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.22 as builder
WORKDIR /go/src/github.com/openshift/cli-manager
COPY . .
RUN make build --warn-undefined-variables

FROM registry.redhat.io/rhel9-4-els/rhel:9.4
COPY --from=builder /go/src/github.com/openshift/cli-manager/cli-manager /usr/bin/
COPY --from=builder /usr/bin/git /usr/bin/git
RUN mkdir /licenses
COPY --from=builder /go/src/github.com/openshift/cli-manager/LICENSE /licenses/.

LABEL com.redhat.component="CLI Manager"
LABEL cpe="cpe:/a:redhat:cli_manager_operator:0.1::el9"
LABEL description="The CLI Manager is a comprehensive tool designed to simplify the management of OpenShift CLI plugins within the OpenShift environment. Modeled after the popular krew plugin manager, it offers seamless integration, easy installation, and efficient handling of a wide array of plugins, enhancing your OpenShift command-line experience."
LABEL name="cli-manager/cli-manager-rhel9"
LABEL summary="The CLI Manager is a comprehensive tool designed to simplify the management of OpenShift CLI plugins within the OpenShift environment. Modeled after the popular krew plugin manager, it offers seamless integration, easy installation, and efficient handling of a wide array of plugins, enhancing your OpenShift command-line experience."
LABEL io.k8s.display-name="CLI Manager" \
      io.k8s.description="The CLI Manager is a comprehensive tool designed to simplify the management of OpenShift CLI plugins within the OpenShift environment. Modeled after the popular krew plugin manager, it offers seamless integration, easy installation, and efficient handling of a wide array of plugins, enhancing your OpenShift command-line experience." \
      io.openshift.tags="openshift,cli-manager"
USER 1001

