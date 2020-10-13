#!/bin/bash -e

REGISTRY=${REGISTRY:=localhost:5000}
IMAGE_TAG=${IMAGE_TAG:=0.9}
SHIPPER_IMAGE=${SHIPPER_IMAGE:=${REGISTRY}/shipper:${IMAGE_TAG}}
SHIPPER_STATE_METRICS_IMAGE=${SHIPPER_STATE_METRICS_IMAGE:=${REGISTRY}/shipper-state-metrics:${IMAGE_TAG}}

if [[ "${1}" == "--help" ]]; then
    cat <<EOF

Usage: <env-vars> $(basename "$0")

Possible environment variables:
  SPIN_CLUSTERS                 spin new kind-mgmt and kind-app clusters
                                  (options: "false", "true", defaults to false)
  INSTALL                       install shipper on kind clusters, kind-mgmt and kind-app
                                  (options: "false", "true", defaults to false)
                                  if set to true, it will install shipper on kind-mgmt cluster
  REGISTRY                      defaults to "localhost:5000"
  IMAGE_TAG                     defaults to "0.9"
  SHIPPER_IMAGE                 defaults to ${REGISTRY}/shipper:${IMAGE_TAG}
  SHIPPER_STATE_METRICS_IMAGE   defaults to ${REGISTRY}/shipper-state-metrics:${IMAGE_TAG}

Examples:
  SPIN_CLUSTERS=true INSTALL=true $(basename "$0")  ## will create kind-mgmt and kind-app clusters, \
set them up and install shipper on them with kind-mgmt as the management cluster
EOF
    exit 0
fi

SPIN_CLUSTERS=${SPIN_CLUSTERS:="false"}
if [[ ${SPIN_CLUSTERS} == "true" ]]; then
    # spin kind clusters
    read -n1 -r -p " =============== Press space to spin kind-app and kind-mgmt clusters..." cluster_key
    if [[ "$cluster_key" = '' ]]; then
        # echo [$key] is empty when SPACE is pressed # uncomment to trace
        ./hack/kind-with-registry.sh app
        ./hack/kind-with-registry.sh mgmt
    fi
fi

kubectl config use-context kind-mgmt

# setup clusters
SETUP_MGMT_FLAGS="--webhook-ignore" make clean setup


INSTALL=${INSTALL:="false"}
if [[ ${INSTALL} == "true" ]]; then
    read -n1 -r -p " =============== Press space to install shipper on kind-mgmt cluster..." install_key
    if [[ "$install_key" = '' ]]; then
        DOCKER_REGISTRY=${REGISTRY} IMAGE_TAG=${IMAGE_TAG} SHIPPER_IMAGE=${SHIPPER_IMAGE} make install

        # fix kind-app clusters:
        for node in $(kind get nodes --name app); do
          APIMASTER=$(kind get kubeconfig --name app --internal | grep server | grep -Eo 'https.*')
          PATCH="{\"spec\":{\"apiMaster\":\"${APIMASTER}\"}}"
          kubectl patch clusters kind-app --type=merge -p "${PATCH}"
        done
    fi
fi
