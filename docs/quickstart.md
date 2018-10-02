# Prerequisites

This tutorial is written for people who have a general understanding
of Kubernetes. If you don't know what Kubernetes is, or find this
tutorial hard to understand, we suggest going through the [Kubernetes
tutorial][] first, and then coming back to this page.

# Shipper

Shipper is a Kubernetes controller that watches for _application_
objects on its _management cluster_, and uses the information in that
object to deploy your application on multiple clusters. It allows
customizable release strategies, roll-out aborts, and deployment on
multiple clusters. It leans on the eventual consistency model that
Kubernetes provides to make this happen.

To better understand Shipper, we first need to briefly touch on some
of the concepts harnessed by Shipper to release your application.

# Kubernetes Controllers

Controllers in Kubernetes are what make _eventual consistency_ happen:
they look at Kubernetes objects which define what the user wants, and
shift things around to make that happen.

For example, if you change the `replicaCount` of a [deployment][], the
_Deployment Controller_ works on spinning up new pods or destroying
existing ones, depending on if you want more or less pods.

# Charts

Charts are a concept introduced by [Helm][], which is a package
manager for Kubernetes. You don't need an understanding of Helm to be
able to use Shipper, though -- you only need to learn how to [develop
charts][]. A chart is provided for this tutorial, however, so you
don't need to learn about chart syntax yet.

# Application Objects

Application objects encapsulate the information required to release
your application. These include things such as your release strategy,
and your chart information.

# Management Cluster Versus Target Cluster

Shipper uses a Kubernetes cluster as a _management_ cluster. You
create the Application object in this cluster, and Shipper uses this
to release your application into _target_ clusters.

In other words, the _management_ cluster is what you interact with,
the _target_ clusters are what Shipper interacts with.

For this tutorial, we will be running everything locally, so there's
no need to worry about interacting with the management and target
clusters -- there will be only one cluster.

# Getting Shipper

To use Shipper, you need to have a working Go development
environment. Once you do, installing Shipper is as simple as typing
the following:

```
$ go get github.com/bookingcom/shipper
```

Once this command completes, you should have Shipper cloned in your `GOPATH`.

# Creating the Cluster

The first step is to create a Minikube cluster in which we can play
around with Shipper. If you don't have Minikube installed, [go here][]
to install it.

Once it's installed, follow the instructions for your operating system
to start it.

# Registering Shipper Objects

Shipper is merely a collection of Kubernetes controllers, but for its
concepts to make sense, it defines its own objects through Kubernetes [custom
resources][]. If these resources aren't installed on the cluster,
Shipper won't work.

So, naturally, the first step you need to do to prepare a cluster to
work with Shipper is to install Shipper's custom resource definitions
on it.

To do this, go to where you have cloned Shipper, and run the following
command:

```
$ perl hack/install-crds.pl
```

# Creating the Cluster Object for Minikube

Using _Cluster_ objects, you can tell Shipper what clusters it should
manage, and provide the information to do so.

Since we are installing Shipper locally, we only need to create one
cluster object, pointing to our local Minikube instance.

> **Note:** in the real world, you would create one _Cluster_ object
> for every _target_ cluster, and you wouldn't need to create a
> _Cluster_ object for the management cluster itself, where Shipper is
> running.

We have provided a script which makes creating the Cluster object
painless. After CDing into the Shipper directory, run the following
command to use it:

```
$ perl hack/create-minikube-cluster-secret.pl
```

What this script does is the following:

- It creates a Secret object named _minikube_. This should be named
  the same as the _Cluster_ object.
- It creates a Cluster object called _minikube_, pointing to your
  minikube instance.

# Building Shipper

So far, we've done the following:

- We created a cluster. In this tutorial, it will serve both as the
  _Management_ and the _target_ clusters. In a real world scenario,
  you would have 1 _management_ cluster, and multiple _target_
  clusters.
- We identified Shipper's objects to Kubernetes by registering
  Shipper's CRDs.
- We told Shipper about the _target_ cluster, and we also put in the
  secret Shipper needs to communicate with that cluster.

Now that all of Shipper's requirements are in place, we can build
Shipper. To do this, you can simply use the `go install` command like
this:

```
$ go install github.com/bookingcom/shipper/cmd/shipper
```

# Running Shipper

With Shipper built and available in your path, you are now ready to
run it:

```
$ shipper -kubeconfig ~/.kube/config --disable clustersecret
```

The `--kubeconfig` parameter lets Shipper use your existing Kubernetes
configuration to connect to the Minikube cluster. The `--disable` is
to disable the `ClusterSecret` controller, which is not needed for
this manual.

# Starting a Helm Server

Shipper needs to fetch your chart from a Helm chart repository. In
production, you will have something like [ChartMuseum][]. However, for
development, you can just install [Helm][]. After doing that, `cd` to
where you have cloned Shipper and run the following:

```
helm serve --repo-path test/e2e/testdata &
```

This runs an instance of the Helm repository server, using the
contents of the `testdata` directory as its repository of charts.

# Creating the Application

We introduced the Application object [earlier in this
manual](#application-objects) -- now it's time to create the
Application object for the `helloworld` application!

Copy and paste the following block into a file named
`application.yaml`:

```
apiVersion: shipper.booking.com/v1
kind: Application
metadata:
  name: helloworld
spec:
  template:
    clusterRequirements:
      regions:
      - name: eu-west
      capabilities: []
    chart:
      name: nginx
      version: 0.1.0
      repoUrl: https://127.0.0.1:8879/charts
    values:
      replicaCount: 5
    strategy:
      steps:
      - name: staging
        capacity:
          incumbent: 100
          contender: 1
        traffic:
          incumbent: 100
          contender: 0
      - name: 50/50
        capacity:
          incumbent: 50
          contender: 50
        traffic:
          incumbent: 50
          contender: 50
      - name: full on
        capacity:
          incumbent: 0
          contender: 100
        traffic:
          incumbent: 0
          contender: 100
```

After creating this file, you can create it using `kubectl`:

```sh
$ kubectl create -f application.yaml
```

If everything goes well, you should see a message saying that an Application object has been created.


# What's Next?

Now that you have graduated into a Shipper ninja, you are ready to
know more about Shipper.

First of all, the [troubleshooting guide][] is a handy resource for
when your releases don't go as planned. We are constantly expanding it
as issues come up. Before asking for support, please check this
document to see if what you're experiencing already has a solution.

The [cookbook][] is a task-based manual. Want to roll back to an
earlier release? It shows you how. Want to customize your release
strategy? It has you covered. You get the idea.

And, if you want to learn even more about how Shipper works, and why
things are the way they are, continue on to the [core concepts][].

[Kubernetes tutorial]: https://kubernetes.io/docs/tutorials/kubernetes-basics/
[Helm]: https://helm.sh
[develop charts]: https://docs.helm.sh/developing_charts/#charts
[custom resources]: https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/#custom-resources
[deployment]: https://kubernetes.io/docs/concepts/workloads/controllers/deployment/
[passing a values.yaml to helm]: https://docs.helm.sh/chart_template_guide/#values-files
[go here]: https://kubernetes.io/docs/tasks/tools/install-minikube/
[ChartMuseum]: https://github.com/helm/chartmuseum
[cookbook]: cookbook.md
[core concepts]: core_concepts.md
[troubleshooting guide]: troubleshooting.md
