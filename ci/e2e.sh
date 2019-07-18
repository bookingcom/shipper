#!/bin/bash -x

set -e

mkdir ~/.kube
microk8s.config > ~/.kube/config
kubectl create ns shipper-system
kubectl create ns global-rollout-blocks
perl hack/install-crds.pl
go run cmd/create-cluster-secret/main.go --api-server http://127.0.0.1:8080


go run cmd/shipper/*.go --kubeconfig ~/.kube/config --disable clustersecret --resync "$1" --log_dir /tmp &
SHIPPER_PID=$!

go test ./test/e2e --test.v --e2e --kubeconfig ~/.kube/config --testcharts $PWD/test/e2e/testdata/\*.tgz --progresstimeout=2m --appcluster local
TEST_STATUS=$?

set +e

kill $SHIPPER_PID
wait

cat /tmp/*.{WARNING,ERROR}

exit $TEST_STATUS
