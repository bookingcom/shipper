#!/bin/bash -x

set -e

mkdir ~/.kube
microk8s.config > ~/.kube/config

go run cmd/shipperctl/main.go admin clusters apply -f ci/clusters.yaml

# Build the Docker image and push it to the local microk8s registry
microk8s.enable registry
CGO_ENABLED=0 GOOS=linux go build -o shipper cmd/shipper/*.go
docker build -t localhost:32000/bookingcom/shipper:$TRAVIS_COMMIT -f Dockerfile.shipper .
docker push localhost:32000/bookingcom/shipper:$TRAVIS_COMMIT
sed s=\<IMAGE\>=localhost:32000/bookingcom/shipper:$TRAVIS_COMMIT= kubernetes/shipper.deployment.yaml | kubectl create -f -

go test ./test/e2e --test.v --e2e --kubeconfig ~/.kube/config --testcharts $PWD/test/e2e/testdata/\*.tgz --progresstimeout=2m --appcluster microk8s
TEST_STATUS=$?

set +e

kubectl delete deployment shipper

# cat /tmp/*.{WARNING,ERROR}

exit $TEST_STATUS
