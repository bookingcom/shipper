# Shipper E2E tests

## Setup

These tests assume that they can reach a properly configured Kubernetes cluster
that has Shipper running on it. To setup microk8s into this condition, do
the following:
* If you don't have a `container-registry` namespace, run `microk8s.enable registry` in your microk8s
* In microk8s open file `/var/snap/microk8s/current/args/kube-apiserver`. Look for `--insecure-port=XXXX` (for example 8080)
* In you computer `kubectl config view`. Look for microk8s-cluster, and make sure that the server 
port is the __insecure__ port from your microk8s (and not the __secure__ port)!
* Disable proxys should help
* Make sure you're using microk8s context (`kubectl config use-context microk8s`)
* Use shipperctl to configure clusters: `go run cmd/shipperctl/main.go admin clusters apply -f ci/clusters.yaml`
* Wait for microk8s.registry to be ready, as we're about to use it
    ```
    REGISTRY_POD=$(kubectl get pod -n container-registry -l app=registry -o jsonpath='{.items[0].metadata.name}')
    kubectl wait -n container-registry --for=condition=ready pod/$REGISTRY_POD
    ```
* Build an image with the test charts and deploy it:
    ```
    HELM_IMAGE=$REGISTRY/shipper-helm:latest make helm
    sed s=\<HELM_IMAGE\>=$REGISTRY/shipper-helm:latest= ci/helm.yaml | \
    kubectl apply -f -
    ```
    * replace `$REGISTRY` with the proper image registry. You can use `localhost:32000` if you're 
    running in you microk8s, `MICROK8S-IP:MICROK8S-PORT` if you're running shipper locally, 
    or use a public registry like docker-registry.
* Build an image with shipper and deploy it (again, replace `$REGISTRY`): 
    ```
    SHIPPER_IMAGE=$REGISTRY/shipper:latest make shipper
    sed s=\<IMAGE\>=$REGISTRY/shipper:latest= kubernetes/shipper.deployment.yaml | \
    kubectl create -f -
    ```

Once you have a working cluster (and shipper pod has a 'ready' condition), you can run the e2e tests like so:

```
TESTCHARTS=http://$(kubectl get pod -l app=helm -o jsonpath='{.items[0].status.podIP}'):8879
go test ./test/e2e --test.v --e2e --kubeconfig ~/.kube/config --testcharts $TESTCHARTS --progresstimeout=2m --appcluster microk8s
```

Alternatively, if you'd like to use the e2e tests as part of your development
workflow, I'd suggest compiling the test binary:

```
go test -c ./test/e2e -kubeconfig ~/.kube/config -e2e
```

And then running it at whatever frequency you find useful:

```
./e2e.test -kubeconfig ~/.kube/config -e2e
```

Adding the `-v` switch will include verbose logs about each step of each test.

If the tests are failing and you want to investigate, add `-inspectfailed`:
this will not delete the test namespace for failed tests: this allows you to go
poke around with `kubectl`. Once you're done debugging, remove all the
namespaces with `kubectl delete ns -l shipper-e2e-test`.
