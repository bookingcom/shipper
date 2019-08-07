SHIPPER_IMAGE ?= bookingcom/shipper:latest
METRICS_IMAGE ?= bookingcom/shipper-state-metrics:latest
HELM_IMAGE ?= bookingcom/shipper-helm:latest
SHIPPER_NAMESPACE ?= shipper-system
KUBECTL ?= kubectl -n $(SHIPPER_NAMESPACE)

PKG = pkg/**/* vendor/**/*

export GOFLAGS := -mod=vendor
export GO111MODULE := on
export CGO_ENABLED := 0
export GOARCH := amd64
export GOOS := linux

build/%: cmd/%/*.go $(PKG)
	go build -o $@ cmd/$*/*.go

build/e2e.test: $(PKG) test/e2e/*
	go test -c ./test/e2e/ -o build/e2e.test

build: build/shipper build/shipperctl build/shipper-state-metrics build/e2e.test

.PHONY: shipper shipper-state-metrics restart logs helm lint test vendor

shipper: build/shipper Dockerfile.shipper
	docker build -f Dockerfile.shipper -t $(SHIPPER_IMAGE) --build-arg HTTP_PROXY=$(HTTP_PROXY) --build-arg HTTPS_PROXY=$(HTTPS_PROXY) .
	docker push $(SHIPPER_IMAGE)

shipper-state-metrics: build/shipper-state-metrics Dockerfile.shipper-state-metrics
	docker build -f Dockerfile.shipper-state-metrics -t $(METRICS_IMAGE) --build-arg HTTP_PROXY=$(HTTP_PROXY) --build-arg HTTPS_PROXY=$(HTTPS_PROXY) .
	docker push $(METRICS_IMAGE)

helm:
	docker build -f Dockerfile.helm -t $(HELM_IMAGE) --build-arg HTTP_PROXY=$(HTTP_PROXY) --build-arg HTTPS_PROXY=$(HTTPS_PROXY) .
	docker push $(HELM_IMAGE)

restart:
	# Delete all Pods in namespace, to force the ReplicaSet to spawn new ones
	# with the new latest image (assuming that imagePullPolicy is set to Always).
	$(KUBECTL) delete pods --all

logs:
	$(KUBECTL) get po -o jsonpath='{.items[*].metadata.name}' | xargs $(KUBECTL) logs --follow

lint:
	golangci-lint run -v --config .golangci.yml ./pkg/... ./cmd/... ./test/...

test:
	go test -v ./pkg/... ./cmd/...

vendor:
	go mod tidy -v
	go mod vendor -v
	go mod verify
