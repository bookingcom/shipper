#!/bin/bash -e

# Do not set -x here.

if [ "$TRAVIS_PULL_REQUEST" != "false" ]; then
    echo "Not running packaging for PR '$TRAVIS_PULL_REQUEST'"
    exit 0
fi

if [ "$TRAVIS_BRANCH" != "master" ]; then
    echo "Not running packaging for branch '$TRAVIS_BRANCH'"
    exit 0
fi

if [ "$TRAVIS_SECURE_ENV_VARS" != "true" ]; then
    echo "TRAVIS_SECURE_ENV_VARS not set, refusing to continue" >&2
    exit 1
fi

echo "$DOCKER_PASSWORD" | docker login -u "$DOCKER_USERNAME" --password-stdin

docker build -t bookingcom/shipper:$TRAVIS_COMMIT -f Dockerfile.shipper .
docker push bookingcom/shipper:$TRAVIS_COMMIT

docker build -t bookingcom/shipper-state-metrics:$TRAVIS_COMMIT -f Dockerfile.metrics .
docker push bookingcom/shipper-state-metrics:$TRAVIS_COMMIT

docker logout

unset DOCKER_USERNAME
unset DOCKER_PASSWORD
