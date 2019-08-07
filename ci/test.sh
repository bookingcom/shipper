#!/bin/bash -ex

go mod verify

./hack/verify-codegen.sh

make lint test
