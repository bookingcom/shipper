SHIPPER_IMAGE = bookingcom/shipper:latest
METRICS_IMAGE = bookingcom/shipper-state-metrics:latest
SHIPPER_NAMESPACE = shipper-system
KUBECTL = kubectl -n $(SHIPPER_NAMESPACE)

.PHONY: shipper

shipper:
	GOARCH=amd64 CGO_ENABLED=0 GOOS=linux go build -o shipper ./cmd/shipper/*.go
	docker build -f Dockerfile.shipper -t $(SHIPPER_IMAGE) --build-arg HTTP_PROXY=$(HTTP_PROXY) --build-arg HTTPS_PROXY=$(HTTPS_PROXY) .
	docker push $(SHIPPER_IMAGE)

shipper-state-metrics:
	GOARCH=amd64 CGO_ENABLED=0 GOOS=linux go build -o shipper-state-metrics ./cmd/shipper-state-metrics/*.go
	docker build -f Dockerfile.shipper-state-metrics -t $(METRICS_IMAGE) --build-arg HTTP_PROXY=$(HTTP_PROXY) --build-arg HTTPS_PROXY=$(HTTPS_PROXY) .
	docker push $(METRICS_IMAGE)

restart:
	# Delete all Pods in namespace, to force the ReplicaSet to spawn new ones
	# with the new latest image (assuming that imagePullPolicy is set to Always).
	$(KUBECTL) delete pods --all

logs:
	$(KUBECTL) get po -o jsonpath='{.items[*].metadata.name}' | xargs $(KUBECTL) logs --follow
