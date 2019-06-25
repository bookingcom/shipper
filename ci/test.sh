#!/bin/bash -ex

go mod verify

./hack/verify-codegen.sh

PKGS="./pkg/... ./cmd/... ./test/..."

golangci-lint run --config .golangci.yml $PKGS

go test -v $PKGS

./ci/e2e.sh "$RESYNC_PERIOD"
