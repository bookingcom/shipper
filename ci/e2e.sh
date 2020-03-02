#!/bin/bash -x

kubectl config use-context kind-mgmt

# Setup shipper's clusters
make setup

# Run the e2e tests, save exit code for later
make -j e2e \
	TEST_HELM_REPO_URL=${TEST_HELM_REPO_URL:=https://raw.githubusercontent.com/bookingcom/shipper/${TRAVIS_COMMIT}/test/e2e/testdata} \
	DOCKER_REGISTRY=${DOCKER_REGISTRY:=registry:5000} \
	E2E_FLAGS="--test.v"

TEST_STATUS=$?

# Remove yaml artifacts that we no longer need, so they don't end up in
# releases
rm -f build/*.latest.yaml

# Output all of the logs from the shipper pods, for reference
kubectl -n shipper-system logs $(kubectl -n shipper-system get pod -l component=shipper-app -o jsonpath='{.items[0].metadata.name}')
kubectl -n shipper-system logs $(kubectl -n shipper-system get pod -l component=shipper-mgmt -o jsonpath='{.items[0].metadata.name}')

# Exit with the exit code we got from the e2e tests
exit $TEST_STATUS
