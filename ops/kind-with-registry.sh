#!/bin/sh
set -o errexit

NAME=$1

if [[ -z "$NAME" ]] || [[ "${1}" == "--help" ]]; then
    cat <<EOF
Usage: $(basename "$0") <cluster-name>

  <cluster-name>        the name of the kind cluster to create. the cluster will be named "kind-<cluster-name>".

Examples:
  $(basename "$0") mgmt ## will create a kind-mgmt cluster
EOF
    exit 0
fi

# create registry container unless it already exists
reg_name='kind-registry'
reg_port='5000'
running="$(docker inspect -f '{{.State.Running}}' "${reg_name}" 2>/dev/null || true)"
if [[ "${running}" != 'true' ]]; then
  docker run \
    -d --restart=always -p "${reg_port}:5000" --name "${reg_name}" \
    registry:2
fi

# create a cluster with the local registry enabled in containerd
cat <<EOF | kind create cluster --name ${NAME} --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:${reg_port}"]
    endpoint = ["http://${reg_name}:${reg_port}"]
EOF


# connect the registry to the cluster network
docker network connect "kind" "${reg_name}" || NET_CONNECT=$?
echo ${NET_CONNECT}

# tell https://tilt.dev to use the registry
# https://docs.tilt.dev/choosing_clusters.html#discovering-the-registry
for node in $(kind get nodes --name ${NAME}); do
  kubectl annotate node "${node}" "kind.x-k8s.io/registry=localhost:${reg_port}" || ANNOTATE=$?
  echo ${ANNOTATE}
  echo "internal ip:"
  cmd="cat /etc/hosts | grep $node"
  docker exec ${node} sh -c "${cmd}"
  kind get kubeconfig --name ${NAME} --internal | grep server
done