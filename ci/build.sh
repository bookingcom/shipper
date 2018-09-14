#!/bin/bash -x

CGO_ENABLED=0 GOOS=linux go build -v -o shipper cmd/shipper/*.go
CGO_ENABLED=0 GOOS=linux go build -v -o shipper-state-metrics cmd/shipper-state-metrics/*.go