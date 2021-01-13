#!/bin/bash -ex

if [ "$TRAVIS_BRANCH" != "master" ] && [ "$TRAVIS_TAG" == "" ]; then
    echo "Not running release for branch '$TRAVIS_BRANCH'"
    exit 0
fi

if [ "$TRAVIS_SECURE_ENV_VARS" != "true" ]; then
    echo "TRAVIS_SECURE_ENV_VARS not set, refusing to continue" >&2
    exit 1
fi

echo "$DOCKER_PASSWORD" | docker login -u "$DOCKER_USERNAME" --password-stdin

USE_IMAGE_NAME_WITH_SHA256= IMAGE_TAG=$TRAVIS_TAG make build-all

docker logout

unset DOCKER_USERNAME
unset DOCKER_PASSWORD

