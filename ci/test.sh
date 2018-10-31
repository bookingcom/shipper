#!/bin/bash -ex

dep status -v

./hack/verify-codegen.sh

gometalinter \
    --vendor \
    --skip=pkg/apis/shipper/v1 \
    --skip=pkg/client \
    --disable-all \
    --enable=vet \
    --enable=ineffassign \
    --enable=deadcode \
    --enable=goconst \
    --enable=megacheck \
    ./...

go test -v $(go list ./... | grep -v /vendor/)

./ci/e2e.sh "$RESYNC_PERIOD"
