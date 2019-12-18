#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

# PROJECT_ROOT=$(cd $(dirname "$0")/.. ; pwd)
# CODEGEN_PKG=${PROJECT_ROOT}/vendor/k8s.io/kube-openapi
# VERSION="v1alpha1"
# 
# go run ${CODEGEN_PKG}/cmd/openapi-gen/openapi-gen.go \
#   --go-header-file ${PROJECT_ROOT}/hack/custom-boilerplate.go.txt \
#   --input-dirs github.com/bookingcom/shipper/pkg/apis/workflow/${VERSION} \
#   --output-package github.com/argoproj/argo/pkg/apis/workflow/${VERSION} \
#   --report-filename pkg/apis/api-rules/violation_exceptions.list \
#   $@
# PROJECT_ROOT=$(cd $(dirname "$0")/.. ; pwd)
# CODEGEN_PKG=${PROJECT_ROOT}/vendor/k8s.io/kube-openapi
VERSION="v1alpha1"

go run $GOPATH/src/k8s.io/kube-openapi/cmd/openapi-gen/openapi-gen.go \
  --input-dirs github.com/bookingcom/shipper/pkg/apis/shipper/${VERSION} \
  --output-package github.com/bookingcom/shipper/pkg/apis/shipper/${VERSION} \
  --report-filename pkg/apis/api-rules/violation_exceptions.list
  $@

