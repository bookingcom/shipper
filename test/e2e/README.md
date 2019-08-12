# Shipper E2E tests

## Setup

These tests assume that they can reach a properly configured Kubernetes cluster
that has Shipper running on it, so you'll have to procure yourself one.
[Microk8s](http://microk8s.io) works fine, as does [Docker
Desktop](https://www.docker.com/products/docker-desktop) with Kubernetes
enabled, but ultimately anything reasonably recent will do.

You'll also need a Docker registry where you can freely upload Docker images
to. [You can get one with your Microk8s
installation](https://microk8s.io/docs/working#working-with-microk8s-registry-add-on)
fairly easily, but figuring it out for other environments is left as an
exercise to the reader.

Last but not least, you'll also need to craft a cluster spec YAML. If you're
using a stock microk8s configuration, you can use the one in
`ci/clusters.yaml`. Docker Desktop is a bit more specific, so you'll need to
use the configuration below. It's wise not to keep this file in your working
directory, so choose somewhere else that's handy, and point
`SHIPPER_CLUSTERS_YAML` (described later) to it.

```yaml
managementClusters:
- name: docker-desktop
applicationClusters:
- name: docker-desktop
  region: local
  apiMaster: https://kubernetes.docker.internal:6443
```

Once you have figured out your cluster and registry, it's useful to export them
as a variable that our Makefile can make use of, as below. If you don't want to
type that in every new shell session, you can put it in your `~/.bashrc` or
`~/.zshrc`.

```shell
# URL to your registry. If you have microk8s installed on your own machine,
# this should be localhost:32000. Adjust if you have microk8s in a separate
# machine, or a different registry altogether.
export DOCKER_REGISTRY=localhost:32000

# Name of the context for the cluster where you want shipper to run. `kubectl
# config get-contexts` will help you figure it out.
export SHIPPER_CLUSTER=microk8s

# Path to your cluster spec YAML. "ci/clusters.yaml" is the default, so you can
# omit this if you're using microk8s.
export SHIPPER_CLUSTERS_YAML=ci/clusters.yaml
```

With everything properly configured, it's time to setup the clusters for
shipper. That's done with the command below. You only need to run it once, or
every time you change your `$SHIPPER_CLUSTERS_YAML` file.

```shell
make setup
```

To run the end-to-end tests, now, you only need to run the following:

```shell
make -j e2e
```

This will compile the necessary binaries, build all the Docker images, deploy
them to your cluster, and run the tests. Hopefully you'll be lucky and the
tests will pass, too!

In case you're unlucky, and the tests do not pass, you'll need to investigate.
The following calls to `make e2e` can help you with that. You can combine any
of the `E2E_FLAGS` into a single call, as they're just parameters that get
passed to the `e2e.test` executable.

```shell
# Enable verbose output for every test, not only the ones that fail.
E2E_FLAGS="--test.v" make -j e2e

# Do not delete namespaces for failed tests, so you can inspect them manually
# with `kubectl`.
E2E_FLAGS="-inspectfailed" make -j e2e
```
