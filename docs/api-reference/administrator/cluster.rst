.. _api-reference_cluster:

#######
Cluster
#######

A *Cluster* object represents a Kubernetes cluster that Shipper can deploy to.
It is an **administrative** interface.

They serve two purposes:

* Enable Shipper to connect to the cluster to manage it
* Enable administrators to influence how *Releases* are scheduled to this cluster.

The second point allows administrators to perform tasks like load balancing
workloads between clusters, shift workloads from one cluster to another, or
drain clusters for risky maintenance. For examples of these tasks, see the
:ref:`administrator's guide <operations_fleet-management>`.

*******
Example
*******

.. literalinclude:: ../../examples/cluster.yaml
    :language: yaml
    :linenos:

****
Spec
****

``.spec.apiMaster``
===================

``apiMaster`` is the URL of the Kubernetes cluster API server. Shipper uses
this to connect to the cluster to manage it. This is the same URL as in
a ``~/.kube/config`` for enabling ``kubectl`` commands.

.. _api-reference_cluster_capabilities:

``.spec.capabilities``
======================

``capabilities[]`` is a required field that lists the capabilities the
cluster has. Capabilities are arbitrary tags that can be used by Application
objects to select clusters while rolling out. For example, one Kubernetes
cluster might have nodes provisioned with GPUs for video encoding. Adding 'gpu'
as a Cluster capability will allow application developers to specify 'gpu' in
their set of Application ``clusterRequirements`` if their application needs
access to that feature.

``.spec.region``
================

``region`` is a required field that specifies the region the cluster belongs to.

``.spec.scheduler``
===================

``scheduler.unschedulable`` is an optional field that causes clusters to
be ignored during rollout cluster selection. This allows operators to mark
clusters to be drained. Default: ``false``.

``scheduler.weight`` is an optional field that assigns a weight to the
cluster. The weight influences the priority of the cluster during rollout
cluster selection. Default: ``100``.

``scheduler.identity`` is an optional field that assigns an identity to
the cluster different than its ``.metadata.name`` value. This allows operators
to make one cluster 'impersonate' another in order to transfer all of the
Applications on one cluster to another specific cluster. Default:
``.metadata.name``.

More information on how to use these fields to manage a fleet of clusters can
be found in the :ref:`Administrator's guide <operations_fleet-management>`.

******
Status
******

Cluster objects do not currently have a meaningful ``.status`` field.
