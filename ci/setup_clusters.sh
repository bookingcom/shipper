#!/bin/bash -ex

# desired cluster name; default is "kind"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-kind}"

# create registry container unless it already exists
reg_name='kind-registry'
reg_port='5000'
running="$(docker inspect -f '{{.State.Running}}' "${reg_name}" 2>/dev/null || true)"
if [ "${running}" != 'true' ]; then
	docker run \
		-d --restart=always -p "${reg_port}:5000" --name "${reg_name}" \
		registry:2
fi

kind create cluster --name $KIND_CLUSTER_NAME --config ci/kind.yaml --image kindest/node:v1.15.7

# add the registry to /etc/hosts on each node
ip_fmt='{{.NetworkSettings.IPAddress}}'
cmd="echo $(docker inspect -f "${ip_fmt}" "${reg_name}") registry >> /etc/hosts"
for node in $(kind get nodes --name "${KIND_CLUSTER_NAME}"); do
	docker exec "${node}" sh -c "${cmd}"
done

# add the registry to /etc/hosts on the host
echo "127.0.0.1 registry kubernetes.default" | sudo tee -a /etc/hosts
