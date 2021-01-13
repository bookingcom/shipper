#!/bin/bash -ex

# create registry container unless it already exists
reg_name='kind-registry'
reg_port='5000'
docker run \
	-d --restart=always -p "${reg_port}:5000" --name "${reg_name}" \
	registry:2

PIDS=""

create_cluster () {
	local CLUSTER=$1
	local CONFIG=/tmp/kind/$CLUSTER

	kind create cluster \
		--name $CLUSTER \
		--config ci/kind.yaml \
		--image kindest/node:v1.17.2 \
		--kubeconfig $CONFIG \
		--quiet

	# get a kubeconfig with an actual ip address instead of 127.0.0.1
	kind get kubeconfig --name $CLUSTER > $CONFIG

	# add the registry to /etc/hosts on each node
	ip_fmt='{{.NetworkSettings.IPAddress}}'
	cmd="echo $(docker inspect -f "${ip_fmt}" "${reg_name}") registry >> /etc/hosts"
	for node in $(kind get nodes --name "${CLUSTER}"); do
		docker exec "${node}" sh -c "${cmd}"
	done

}

for CLUSTER in mgmt app; do
	create_cluster $CLUSTER & PIDS="$PIDS $!"
done

wait $PIDS

mkdir -p ~/.kube
KUBECONFIG=$(find /tmp/kind -type f | tr \\n ':') kubectl config view --flatten >> ~/.kube/config

kubectl config view


# connect the registry to the cluster network
docker network connect "kind" "${reg_name}" || NET_CONNECT=$?
echo ${NET_CONNECT}
# add the registry to /etc/hosts on the host
echo "127.0.0.1 registry kubernetes.default" | sudo tee -a /etc/hosts
