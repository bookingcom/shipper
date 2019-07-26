#!/bin/bash -x

set -e

# Apply cluster configurations for shipper
go run cmd/shipperctl/main.go admin clusters apply -f ci/clusters.yaml

# Wait for microk8s.registry to be ready, as we're about to use it
REGISTRY_POD=$(kubectl get pod -n container-registry -l app=registry \
	-o jsonpath='{.items[0].metadata.name}')
kubectl wait -n container-registry --for=condition=ready pod/$REGISTRY_POD

# Build an image with the test charts and deploy it
HELM_IMAGE=localhost:32000/bookingcom/shipper-helm:latest make helm
sed s=\<HELM_IMAGE\>=localhost:32000/bookingcom/shipper-helm:latest= ci/helm.yaml | \
	kubectl apply -f -

# Build an image with shipper and deploy it
SHIPPER_IMAGE=localhost:32000/bookingcom/shipper:latest make shipper
sed s=\<IMAGE\>=localhost:32000/bookingcom/shipper:latest= kubernetes/shipper.deployment.yaml | \
	kubectl create -f -

SHIPPER_POD=$(kubectl get pod -n shipper-system -l app=shipper \
	-o jsonpath='{.items[0].metadata.name}')
kubectl wait -n shipper-system --for=condition=ready pod/$SHIPPER_POD
kubectl -n shipper-system logs -f $SHIPPER_POD &

TESTCHARTS=http://$(kubectl get pod -l app=helm -o jsonpath='{.items[0].status.podIP}'):8879

go test ./test/e2e --test.v --e2e --kubeconfig ~/.kube/config \
	--testcharts $TESTCHARTS --progresstimeout=2m --appcluster microk8s

TEST_STATUS=$?

set +e

exit $TEST_STATUS
