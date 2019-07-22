# Shipper E2E tests

## Setup

These tests assume that they can reach a properly configured Kubernetes cluster
that has Shipper running on it. To get a fresh minikube into this condition, do
the following:

* `minikube start`
* `perl hack/install-crds.pl`
* `perl hack/create-minikube-cluster-secret.pl`: this creates a secret and cluster object on the minikube that point to itself.
* In the background or a different shell: `go run cmd/shipper/main.go --logtostderr --kubeconfig ~/.kube/config -v 4 --disable clustersecret`

Once you have a working cluster, you can run the e2e tests like so:

```
go test ./test/e2e -kubeconfig ~/.kube/config -e2e`
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
