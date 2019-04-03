#!/bin/bash -x

set -e

mkdir ~/.kube
microk8s.config > ~/.kube/config

go run cmd/shipperctl/main.go admin clusters apply -f ci/clusters.yaml

go run cmd/shipper/*.go --kubeconfig ~/.kube/config --disable clustersecret --resync "$1" --log_dir /tmp &
SHIPPER_PID=$!

go test ./test/e2e --test.v --e2e --kubeconfig ~/.kube/config --testcharts $PWD/test/e2e/testdata/\*.tgz --progresstimeout=2m --appcluster microk8s
TEST_STATUS=$?

set +e

kill $SHIPPER_PID
wait

cat /tmp/*.{WARNING,ERROR}

exit $TEST_STATUS
