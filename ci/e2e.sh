#!/bin/bash

set -ex

kubectl config use-context kind-mgmt

# Setup shipper's clusters
SETUP_MGMT_FLAGS="--webhook-ignore" make setup

# Run the e2e tests, save exit code for later
OVERLAY_PATH=kubernetes/overlays/test make -j e2e \
	TEST_HELM_REPO_URL=${TEST_HELM_REPO_URL:=https://raw.githubusercontent.com/bookingcom/shipper/${GITHUB_SHA}/test/e2e/testdata} \
	DOCKER_REGISTRY=${DOCKER_REGISTRY:=localhost:5000} \
	E2E_FLAGS="--test.v --buildAppClientFromKubeConfig"

TEST_STATUS=$?

# Remove yaml artifacts that we no longer need, so they don't end up in
# releases
rm -f build/*.latest.yaml

# Output all of the logs from the shipper pod, for reference
kubectl -n shipper-system logs $(kubectl -n shipper-system get pod -l app=shipper -o jsonpath='{.items[0].metadata.name}')

# Exit with the exit code we got from the e2e tests
exit ${TEST_STATUS}
