#!/bin/bash -ex

go mod verify

./hack/verify-codegen.sh

gometalinter \
    --vendor \
    --exclude=^vendor\/ \
    --skip=pkg/apis/shipper/v1alpha1 \
    --skip=pkg/client \
    --disable-all \
    --enable=vet \
    --enable=ineffassign \
    --enable=deadcode \
    --enable=goconst \
    --enable=staticcheck \
    ./...

go test -v $(go list ./... | grep -v /vendor/)

./ci/e2e.sh "$RESYNC_PERIOD"
