#!/bin/bash -ex

REGISTRY=${REGISTRY:=localhost:5000}
IMAGE_TAG=${IMAGE_TAG:=0.9}
SHIPPER_MGMT_IMAGE=${SHIPPER_MGMT_IMAGE:=${REGISTRY}/shipper-mgmt:${IMAGE_TAG}}
SHIPPER_APP_IMAGE=${SHIPPER_APP_IMAGE:=${REGISTRY}/shipper-app:${IMAGE_TAG}}

if [[ "${1}" == "--help" ]]; then
    cat <<EOF

Usage: <env-vars> $(basename "$0")

Possible environment variables:
  SPIN_CLUSTERS        spin new kind-mgmt and kind-app clusters
                        (options: "false", "true", defaults to false)
  INSTALL              install shipper-mgmt and shipper-app on kind clusters kind-mgmt and kind-app
                        (options: "false", "true", defaults to false)
                        if set to true, it will imidiatly install shipper-mgmt, but wait for key stroke to install shipper-app
  REGISTRY             defaults to "localhost:5000"
  IMAGE_TAG            defaults to "0.9"
  SHIPPER_MGMT_IMAGE   defaults to ${REGISTRY}/shipper-mgmt:${IMAGE_TAG}
  SHIPPER_APP_IMAGE    defaults to ${REGISTRY}/shipper-app:${IMAGE_TAG}

Examples:
  SPIN_CLUSTERS=true INSTALL=true $(basename "$0")  ## will create kind-mgmt and kind-app clusters, \
set them up and install shipper-mgmt and shipper-app on them
EOF
    exit 0
fi

SPIN_CLUSTERS=${SPIN_CLUSTERS:="false"}
if [[ ${SPIN_CLUSTERS} == "true" ]]; then
    # spin kind clusters
    ./ops/kind-with-registry.sh app
    ./ops/kind-with-registry.sh mgmt
fi

kubectl config use-context kind-mgmt

# setup clusters
SETUP_MGMT_FLAGS="--webhook-ignore" make clean setup

# fix kind-app clusters:
for node in $(kind get nodes --name app); do
  APIMASTER=$(kind get kubeconfig --name app --internal | grep server | grep -Eo 'https.*')
  PATCH="{\"spec\":{\"apiMaster\":\"${APIMASTER}\"}}"
  kubectl patch clusters kind-app --type=merge -p "${PATCH}"
done

INSTALL=${INSTALL:="false"}
if [[ ${INSTALL} == "true" ]]; then
    echo " =============== Installing shipper-mgmt"
    DOCKER_REGISTRY=${REGISTRY} IMAGE_TAG=${IMAGE_TAG} SHIPPER_MGMT_IMAGE=${SHIPPER_MGMT_IMAGE} make install-shipper-mgmt
    read -n1 -r -p " =============== Press space to install shipper-app..." key
    if [[ "$key" = '' ]]; then
        # echo [$key] is empty when SPACE is pressed # uncomment to trace
        DOCKER_REGISTRY=${REGISTRY} IMAGE_TAG=${IMAGE_TAG} SHIPPER_APP_IMAGE=${SHIPPER_APP_IMAGE} make install-shipper-app
    fi
fi
