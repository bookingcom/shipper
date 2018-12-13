SHIPPER_NAMESPACE = shipper-system
KUBECTL = kubectl -n $(SHIPPER_NAMESPACE)

.PHONY: shipper

shipper:
	GOARCH=amd64 CGO_ENABLED=0 GOOS=linux go build -o build/shipper ./cmd/shipper/*.go
	docker build -f Dockerfile.shipper -t localhost:32000/shipper:latest --build-arg HTTP_PROXY=$(HTTP_PROXY) --build-arg HTTPS_PROXY=$(HTTPS_PROXY) .
	docker push localhost:32000/shipper:latest

shipper-state-metrics:
	GOARCH=amd64 CGO_ENABLED=0 GOOS=linux go build -o build/shipper-state-metrics ./cmd/shipper-state-metrics/*.go
	docker build -f Dockerfile.shipper-state-metrics -t localhost:32000/shipper-state-metrics:latest --build-arg HTTP_PROXY=$(HTTP_PROXY) --build-arg HTTPS_PROXY=$(HTTPS_PROXY) .
	docker push localhost:32000/shipper-state-metrics:latest

restart:
	$(KUBECTL) get po -o jsonpath='{.items[*].metadata.name}' | xargs $(KUBECTL) delete po

certs:
	./hack/webhook/webhook-create-signed-cert.sh --namespace $(SHIPPER_NAMESPACE)

install:
	$(KUBECTL) apply -f kubernetes/shipper.service.yaml
	$(KUBECTL) apply -f kubernetes/shipper.deployment.yaml
	cat kubernetes/validating-webhook-configuration.yaml | hack/webhook/webhook-patch-ca-bundle.sh | $(KUBECTL) apply -f -

logs:
	$(KUBECTL) get po -o jsonpath='{.items[*].metadata.name}' | xargs $(KUBECTL) logs --follow
