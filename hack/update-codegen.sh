#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

bash "hack/generate-groups.sh" all \
     github.com/bookingcom/shipper/pkg/client \
     github.com/bookingcom/shipper/pkg/apis \
     shipper:v1alpha1 \
     --go-header-file "hack/boilerplate.go.txt"
