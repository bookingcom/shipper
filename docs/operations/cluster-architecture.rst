.. _operations_cluster-architecture:

Cluster architecture
====================

Shipper defines two kinds of Kubernetes clusters, **management** clusters and
**application** clusters.

*******************
Management clusters
*******************

**Management** clusters are where Shipper itself runs. It has the Shipper
*Custom Resource Definitions* installed, and is where application developers
interact with the *Application* or *Release* objects. The **management**
cluster stores the set of *Cluster* objects and associated *Secrets* that
enable Shipper to connect to the **application** clusters.

Typically you have one of these per large deployment, or one with a standby.

.. _operations_cluster-architecture_application-cluster:

********************
Application clusters
********************

**Application** clusters are where Shipper installs and rolls out user
workloads.

Prior to version 0.9, Shipper did not run any custom software in the
**application** clusters: it only needed a service account and
associated RBAC configuration.

However, to make Shipper more resilient to pod crashes or deletions,
and the management cluster losing connection with **Application**
clusters, it is now required to run Shipper both on **management** and
**application** clusters.

********
Patterns
********

One **management**, many **application**
----------------------------------------

This is the standard arrangement if you have a fleet of Kubernetes clusters
that you would like to manage with Shipper. The single management cluster
provides application developers with a single place to interface with Shipper's
objects and orchestrate their rollouts.

One-and-the-same
----------------

It is totally fine if the **management** cluster and the **application**
cluster are the same. This is how Shipper is developed, and also how you would
use Shipper if you only have a single Kubernetes cluster in your
infrastructure. You can think about this configuration as using Shipper to
provide a better *Deployment* object, but without any multi-cluster federation.

Note that since version 0.9, you will need to run both
**shipper-mgmt** and **shipper-app** on your single cluster.

Multiple **management**, each with own set of **application**
-------------------------------------------------------------

While Shipper fully supports namespaces as units of multi-tenancy, it does not
yet have any way to limit the set of clusters that an Application can select.
So, if your organization has multiple groups of Kubernetes clusters that are
consumed by disjoint sets of users, it might make sense to create
a **management** cluster for each group of **application** clusters that need
strong isolation between each other.
