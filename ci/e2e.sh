#!/bin/bash -x

# Setup shipper's clusters in microk8s
make setup

# Run the e2e tests, save exit code for later
DOCKER_REGISTRY=${DOCKER_REGISTRY:=localhost:32000} E2E_FLAGS="--test.v" make -j e2e
TEST_STATUS=$?

# Remove yaml artifacts that we no longer need, so they don't end up in
# releases
rm -f build/*.latest.yaml

# Output all of the logs from the shipper pod, for reference
kubectl -n shipper-system logs $(kubectl -n shipper-system get pod -l app=shipper -o jsonpath='{.items[0].metadata.name}')

# Exit with the exit code we got from the e2e tests
exit $TEST_STATUS
