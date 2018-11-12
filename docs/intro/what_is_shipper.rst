What is Shipper
===============

Tools offered by Kubernetes are great at dealing with one cluster, but when it comes to multiple clusters, things are not as clean.

When we mention "multi-cluster orchestration", some people may think, "you mean I can't write a *for* loop? Really?"

It is a valid point. However, there are a lot of other problems that come with the territory, and Shipper makes it easier to solve those problems.

More specifically, here is a general overview of what Shipper provides.

What is Shipper from technical point of view?
---------------------------------------------

Shipper is a collection of *Kubernetes controllers* that make it easier to deploy an application to multiple clusters.

In Kubernetes, a controller is a control loop that watches the shared state of the cluster through the API server and makes changes attempting to move the current state towards the desired state.

In the case of Shipper, the current state is an Application object telling Shipper that you want to deploy a Docker image called ``helloworld`` at the ``latest`` tag with ``5`` replicas in clusters that have GPUs. To reach the desired state, Shipper then selects the clusters for you, puts your objects onto those clusters, and with your guidance, goes through the rollout strategy you have defined until your new application has 100% of your desired capacity and traffic.

Deploying to Multiple Clusters
------------------------------

Shipper deploys your application on multiple clusters in different geographical regions and availability zones within those regions.

Since it is compatible with Kubernetes, it works with other Kubernetes providers outside of B.Platform such as Google Kubernetes Engine as well. If you can use ``kubectl`` with it, chances are, you can use Shipper with it as well.

Shipper also provides a general summary of how your pods are doing across all the clusters they are deployed to. If there is a problem, Shipper makes it easier to find it.

Release Management
------------------

Shipper doesn't just copy-paste your code onto multiple clusters for you -- it allows you to customize the rollout strategy fully.

You can specify the amount of capacity Shipper should provide for you in each step of the rollout. Shipper also integrates with Kubernetes to manage the traffic your release gets. A rollout might look something like this:

Staging
  Keep the previous release (the incumbent) at 100% traffic and capacity, but just spin up a pod with my new release (the contender)
50/50
  - Increase capacity of the contender to 50%. Also, decrease the capacity of my incumbent to 50%.
  - Increase traffic for contender to 50%, and decrease the traffic for incumbent to 50%.
Full On
  - Decrease the capacity of the incumbent to 0%, and increase the contender's capacity to 100%. This means that objects such as the ``Deployment`` of the incumbent are still there; they just have their replicas set to ``0``.
  - Decrease the traffic for incumbent to 0%, and increase the traffic for contender to 100%. From now on, the incumbent is not getting any traffic at all.

At any step of this rollout strategy which is completely customizable, you can:

- Abort the rollout and revert to the last known fully operational release
- Tell Shipper to go one step further in the rollout (yes, Shipper doesn't advance the rollout without your permission)

Roll Backs
----------

Since Shipper keeps a record of all your successful releases, it allows you to roll back to an earlier release very easily.

Charts As Input
---------------
`Helm <https://helm.sh>`_, the Kubernetes package manager, makes it very easy to write `charts <https://docs.helm.sh/developing_charts/#charts>`_. Shipper expects you to give it a chart, and provides an intuitive interface to customize the values defined in each chart. This not only means that you can use the power of Helm to write charts, but also means that it is much easier to customize the values your application works with in different environments.
