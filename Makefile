# Defines defaults for building, tagging and pushing our Docker images. You can
# override any of these with environment variables. Most notably, when working
# on shipper, you'll probably want to override DOCKER_REGISTRY to point to a
# private registry available to you.
DOCKER_REGISTRY ?= docker.io
IMAGE_TAG ?= latest
SHIPPER_IMAGE ?= $(DOCKER_REGISTRY)/bookingcom/shipper:$(IMAGE_TAG)
SHIPPER_STATE_METRICS_IMAGE ?= $(DOCKER_REGISTRY)/bookingcom/shipper-state-metrics:$(IMAGE_TAG)

# Defines the namespace where you want shipper to run.
SHIPPER_NAMESPACE ?= shipper-system

# Defines the path to a shipper clusters definition to be used by shipperctl in
# `make setup`. See ci/clusters.yaml for an example, if you need to override
# this in your development environment.
SHIPPER_CLUSTERS_YAML ?= ci/clusters.yaml

# Defines the default application cluster the end-to-end tests will use. To
# find out which clusters you have available to you, `kubectl get clusters`. If
# that errors out, or returns nothing, you probably need to run `make setup`
# first. This value needs to be present in the `applicationClusters` section in
# $(SHIPPER_CLUSTERS_YAML).
SHIPPER_CLUSTER ?= kind-kind

# Defines optional flags to pass to `build/e2e.test` when running end-to-end
# tests. Useful flags are "-inspectfailed" (keep namespaces used for tests that
# filed) and "--test.v" (outputs information about every test, not only failed
# ones).
E2E_FLAGS ?=

# Defines optional flags to pass to `go test` when running unit tests. Most
# useful flag here is `-v`, for verbose output.
TEST_FLAGS ?=

# Defines optional flags to pass to `shipperctl` when running `make setup`.
SETUP_FLAGS ?=

# When set, deployments generated by `make build-yaml` will refer to the
# image's digest instead of a tag. Very useful in development, but not so much
# when building releases.
USE_IMAGE_NAME_WITH_SHA256 ?= 1

# Defines which version shipper is gonna report itself as. By default, it takes
# the latest tag, and annotates it further with the topmost commit if it doesn't
# match the latest tag, and whether the working copy is dirty.
SHIPPER_VERSION ?= $(shell git describe --tags --dirty)

# All the files (*.go and otherwise) that, when changed, trigger rebuilds of
# binaries in the next run of `make`.
PKG := pkg/**/* vendor/**/*

# The binaries we want to build from `cmd/`.
BINARIES := shipper shipperctl shipper-state-metrics

# The operating systems we support. This gets used by `go build` as the `GOOS`
# environment variable.
OS := linux windows darwin

# The operating system where we're currently running. This is just a shorthand
# for a few targets.
GOOS ?= $(shell go env GOOS)

VERSION_PKG := github.com/bookingcom/shipper/pkg/version
LDFLAGS := -ldflags "-X $(VERSION_PKG).Version=$(SHIPPER_VERSION)"

# Setup go environment variables that we want for every `go build` and `go
# test`, to ensure our environment is consistent for all developers.
export GOFLAGS := -mod=vendor
export GO111MODULE := on
export CGO_ENABLED := 0


# *** Common targets ***
# These are the targets you are most likely to use directly, either when
# working on shipper, or via CI scripts.

KUBECTL ?= kubectl -n $(SHIPPER_NAMESPACE)
.PHONY: setup install install-shipper install-shipper-state-metrics e2e restart logs lint test vendor verify-codegen update-codegen clean

# Set up shipper clusters with `shipperctl`. This is probably the first thing
# you should do when starting to work on shipper, as most of everything else
# depends on having a management cluster talking to an application cluster.
setup: $(SHIPPER_CLUSTERS_YAML) build/shipperctl.$(GOOS)-amd64
	./build/shipperctl.$(GOOS)-amd64 admin clusters apply \
		-f $(SHIPPER_CLUSTERS_YAML) \
		--shipper-system-namespace $(SHIPPER_NAMESPACE) \
		$(SETUP_FLAGS)

# Install shipper in kubernetes, by applying all the required deployment yamls.
install: install-shipper install-shipper-state-metrics
install-shipper: build/shipper.image.$(IMAGE_TAG) build/shipper.deployment.$(IMAGE_TAG).yaml
	$(KUBECTL) apply -f build/shipper.deployment.$(IMAGE_TAG).yaml

install-shipper-state-metrics: build/shipper-state-metrics.image.$(IMAGE_TAG) build/shipper-state-metrics.deployment.$(IMAGE_TAG).yaml
	$(KUBECTL) apply -f build/shipper-state-metrics.deployment.$(IMAGE_TAG).yaml

# Run all end-to-end tests. It does all the work necessary to get the current
# version of shipper on your working directory running in kubernetes, so just
# running `make -j e2e` should get you up and running immediately. Do remember
# do setup your clusters with `make setup` though.
e2e: install build/e2e.test
	./build/e2e.test --e2e --kubeconfig ~/.kube/config \
		--appcluster $(SHIPPER_CLUSTER) \
		--testcharts $(TEST_HELM_REPO_URL) \
		$(E2E_FLAGS)

# Delete all pods in $(SHIPPER_NAMESPACE), to force kubernetes to spawn new
# ones with the latest image (assuming that imagePullPolicy is set to Always).
restart:
	$(KUBECTL) delete pods --all

# Tail logs from shipper's pods.
logs:
	$(KUBECTL) logs -l app=shipper -f

# Run all linters. It's useful to run this one before pushing commits ;)
lint:
	golangci-lint run -v --config .golangci.yml ./pkg/... ./cmd/... ./test/...

# Run all unit tests. It's useful to run this one before pushing commits ;)
test:
	go test $(TEST_FLAGS) ./pkg/... ./cmd/...

# Tidy up and vendor dependencies. Run this every time you add or remove
# dependencies, otherwise the CI pipeline will fail to download them, and
# shipper won't build.
vendor:
	go mod tidy -v
	go mod vendor -v
	go mod verify

# Verifies if the auto-generated code in pkg/apis/shipper/v1alpha1 and
# pkg/client is up to date.
verify-codegen:
	GOPATH=$(shell go env GOPATH) ./hack/verify-codegen.sh

# Generates code for the types in pkg/apis/shipper/v1alpha1 and pkg/client.
update-codegen:
	GOPATH=$(shell go env GOPATH) ./hack/update-codegen.sh

# Remove all build artifacts from the filesystem, and all objects installed in
# kubernetes.
.NOTPARALLEL: clean
clean:
	rm -rf build/

# *** build/ targets ***
.PHONY: build-bin build-yaml build-images build-all
SHA = $(if $(shell which sha256sum),sha256sum,shasum -a 256)
build-bin: $(foreach bin,$(BINARIES),build/$(bin).$(GOOS)-amd64)
build-yaml:  build/shipper.deployment.$(IMAGE_TAG).yaml build/shipper-state-metrics.deployment.$(IMAGE_TAG).yaml
build-images: build/shipper.image.$(IMAGE_TAG) build/shipper-state-metrics.image.$(IMAGE_TAG)
build-all: $(foreach os,$(OS),build/shipperctl.$(os)-amd64.tar.gz) build/sha256sums.txt build-yaml build-images

build:
	mkdir -p build

build/shipper-state-metrics.%-amd64: cmd/shipper-state-metrics/*.go $(PKG)
	GOOS=$* GOARCH=amd64 go build $(LDFLAGS) -o build/shipper-state-metrics.$*-amd64 cmd/shipper-state-metrics/*.go

build/shipper.%-amd64: cmd/shipper/*.go $(PKG)
	GOOS=$* GOARCH=amd64 go build $(LDFLAGS) -o build/shipper.$*-amd64 cmd/shipper/*.go

build/shipperctl.%-amd64: cmd/shipperctl/*.go $(PKG)
	GOOS=$* GOARCH=amd64 go build $(LDFLAGS) -o build/shipperctl.$*-amd64 cmd/shipperctl/*.go

build/e2e.test: $(PKG) test/e2e/*
	go test -c ./test/e2e/ -o build/e2e.test

IMAGE_NAME_WITH_SHA256 = $(shell cat build/$*.image.$(IMAGE_TAG))
IMAGE_NAME_TO_USE = $(if $(USE_IMAGE_NAME_WITH_SHA256),$(IMAGE_NAME_WITH_SHA256),$(IMAGE_NAME_WITH_TAG))
build/%.deployment.$(IMAGE_TAG).yaml: kubernetes/%.deployment.yaml build/%.image.$(IMAGE_TAG) build
	sed s=\<IMAGE\>=$(IMAGE_NAME_TO_USE)= $< > $@

build/sha256sums.txt: $(foreach os,$(OS),build/shipperctl.$(os)-amd64.tar.gz) 
	$(SHA) build/*.tar.gz > $@

build/%.tar.gz: build/%
	tar -zcvf $@ -C build $*


# *** Docker Image Targets ***

# Take the "%" from a target (that is now in $*) and use that as part of a
# variable name, that then gets evaluated, kind of like a poor man's hash. So
# if you have "shipper" in $*, IMAGE_NAME_WITH_TAG will read from
# $(SHIPPER_IMAGE).
IMAGE_NAME_WITH_TAG = $($(subst -,_,$(shell echo $* | tr '[:lower:]' '[:upper:]'))_IMAGE)

# The shipper and shipper-state-metrics targets here are phony and
# supposed to be used directly, as a shorthand. They call their close cousins
# in `build/%.image.$(IMAGE_TAG)`, that are *not* phony, as they output the
# fully qualified name to an image that's immutable to a file. This serves two
# purposes:
#
#   - there's no need to manually delete pods from kubernetes to get new images
#   running, as we can use the digest in deployments so `make install` always
#   deploys the most recent image.
#   - when the file with the image name is up to date, it prevents `docker
#   build` from being called at all, as it just tells us that all layers have
#   already been cached and it didn't generate a new image.

.PHONY: shipper shipper-state-metrics
shipper: build/shipper.image.$(IMAGE_TAG)
shipper-state-metrics: build/shipper-state-metrics.image.$(IMAGE_TAG)

build/%.image.$(IMAGE_TAG): Dockerfile.% build/%.linux-amd64
	docker build -f Dockerfile.$* -t $(IMAGE_NAME_WITH_TAG) --build-arg HTTP_PROXY=$(HTTP_PROXY) --build-arg HTTPS_PROXY=$(HTTPS_PROXY) .
	docker push $(IMAGE_NAME_WITH_TAG)
	docker inspect --format='{{index .RepoDigests 0}}' $(IMAGE_NAME_WITH_TAG) > $@
