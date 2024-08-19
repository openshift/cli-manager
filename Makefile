all: build
.PHONY: all

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/images.mk \
	targets/openshift/deps.mk \
)

GO_BUILD_FLAGS :=-tags strictfipsruntime
IMAGE_REGISTRY :=registry.ci.openshift.org

# This will call a macro called "build-image" which will generate image specific targets based on the parameters:
# $0 - macro name
# $1 - target name
# $2 - image ref
# $3 - Dockerfile path
# $4 - context directory for image build# It will generate target "image-$(1)" for building the image an binding it as a prerequisite to target "images".
$(call build-image,cli-manager,$(IMAGE_REGISTRY)/ocp/4.17:cli-manager, ./Dockerfile,.)

clean:
	$(RM) ./cli-manager
.PHONY: clean

install-krew:
	./hack/install-krew.sh
.PHONY: install-krew

GO_TEST_PACKAGES :=./pkg/... ./cmd/...

test-e2e: GO_TEST_PACKAGES :=./test/e2e
# the e2e imports pkg/cmd which has a data race in the transport library with the library-go init code
test-e2e: GO_TEST_FLAGS :=-v -timeout=3h
test-e2e: install-krew test-unit
.PHONY: test-e2e
