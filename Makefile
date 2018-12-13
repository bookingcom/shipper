SHIPPER_NAMESPACE = shipper-system
KUBECTL = kubectl -n $(SHIPPER_NAMESPACE)

build:
	GOARCH=amd64 CGO_ENABLED=0 GOOS=linux go build -o shipper cmd/shipper/main.go
	docker build -f Dockerfile.shipper -t localhost:32000/shipper:latest --build-arg BASE_IMAGE=alpine:3.8 --build-arg HTTP_PROXY=$(HTTP_PROXY) --build-arg HTTPS_PROXY=$(HTTPS_PROXY) .
	docker push localhost:32000/shipper:latest

restart:
	$(KUBECTL) get po -o jsonpath='{.items[*].metadata.name}' | xargs $(KUBECTL) delete po

certs:
	./webhook-create-signed-cert.sh --namespace $(SHIPPER_NAMESPACE)

install:
	$(KUBECTL) apply -f shipper.service.yaml
	$(KUBECTL) apply -f shipper.deployment.yaml

logs:
	$(KUBECTL) get po -o jsonpath='{.items[*].metadata.name}' | xargs $(KUBECTL) logs --follow
