# Shipper E2E tests

## Setup

These tests assume that they can reach a properly configured Kubernetes cluster
that has Shipper running on it. To setup microk8s into this condition, do
the following:

* In microk8s open file `/var/snap/microk8s/current/args/kube-apiserver`. Look for `--insecure-port=XXXX` (for example 8080)
* In you computer `kubectl config view`. Look for microk8s-cluster, and make sure that the server port is the __insecure__ port from your microk8s (and not the __secure__ port)!
* Disable proxys should help
* Make sure you're using microk8s context (`kubectl config use-context microk8s`)
* `perl hack/install-crds.pl`
* Use shipperctl to configure clusters.
    clusters.yaml file should look like:
    ```
    managementClusters:
    - name: microk8s
    applicationClusters:
    - name: microk8s
    region: local
    ```
    and then `shipperctl admin clusters apply -f clusters.yaml`
* In the background or a different shell run shipper locally `go run cmd/shipper/main.go --logtostderr --kubeconfig ~/.kube/config -v 4 -disable clustersecret`
    Leave this running

Once you have a working cluster, you can run the e2e tests like so:

```
go test ./test/e2e -kubeconfig ~/.kube/config -e2e -appcluster microk8s -v
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
