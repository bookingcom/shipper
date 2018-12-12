build:
	GOARCH=amd64 CGO_ENABLED=0 GOOS=linux go build -o shipper cmd/shipper/main.go
	docker build -f Dockerfile.shipper -t localhost:32000/shipper:latest --build-arg BASE_IMAGE=alpine:3.8 --build-arg HTTP_PROXY=$(HTTP_PROXY) --build-arg HTTPS_PROXY=$(HTTPS_PROXY) .
	docker push localhost:32000/shipper:latest

restart:
	kubectl get -n shipper-system po -o jsonpath='{.items[*].metadata.name}' | xargs kubectl -n shipper-system delete po

certs:
	./webhook-create-signed-cert.sh

install:
	kubectl -n shipper-system apply -f shipper.service.yaml
	kubectl -n shipper-system apply -f shipper.deployment.yaml

logs:
	kubectl get -n shipper-system po -o jsonpath='{.items[*].metadata.name}' | xargs kubectl -n shipper-system logs --follow
