#!/bin/bash -x

set -e

# Apply cluster configurations for shipper
./build/shipperctl admin clusters apply -f ci/clusters.yaml

kubectl apply -f ci/helm_svc.yaml

export HELM_IMAGE=${HELM_IMAGE:=localhost:32000/bookingcom/shipper-helm:latest}
export SHIPPER_IMAGE=${SHIPPER_IMAGE:=localhost:32000/bookingcom/shipper:latest}

make helm shipper

sed s=\<HELM_IMAGE\>=$HELM_IMAGE= ci/helm.yaml | kubectl apply -f -
sed s=\<IMAGE\>=$SHIPPER_IMAGE= kubernetes/shipper.deployment.yaml | kubectl apply -f -

TESTCHARTS=http://$(kubectl get service -n shipper-system helm -o jsonpath='{.spec.clusterIP}'):8879
./build/e2e.test --test.v --e2e --kubeconfig ~/.kube/config \
	--testcharts $TESTCHARTS --progresstimeout=2m --appcluster microk8s

TEST_STATUS=$?

SHIPPER_POD=$(kubectl get pod -n shipper-system -l app=shipper \
	-o jsonpath='{.items[0].metadata.name}')
kubectl -n shipper-system logs $SHIPPER_POD

set +e
exit $TEST_STATUS
