#!/bin/bash -x

set -e

kubectl create ns shipper-system
perl hack/install-crds.pl
perl hack/create-minikube-cluster-secret.pl

go run cmd/shipper/*.go --kubeconfig ~/.kube/config --disable clustersecret --resync "$1" --log_dir /tmp &
SHIPPER_PID=$!

go test ./test/e2e --test.v --e2e --kubeconfig ~/.kube/config --testcharts $PWD/test/e2e/testdata/\*.tgz --progresstimeout=2m
TEST_STATUS=$?

set +e

kill $SHIPPER_PID
wait

cat /tmp/*.{WARNING,ERROR}

exit $TEST_STATUS
