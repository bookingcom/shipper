#!/bin/bash -ex

go mod verify

./hack/verify-codegen.sh

PKGS="./pkg/... ./cmd/... ./test/..."

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
    $PKGS

go test -v $(go list $PKGS)

./ci/e2e.sh "$RESYNC_PERIOD"
