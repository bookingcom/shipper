.. _concept_cluster:

Cluster
=======

A *Cluster* object represents a Kubernetes cluster Shipper can use to:

* Select clusters attending application's required capabilities; and
* Deploy applications into, as well as monitor changes of interesting objects from those Kubernetes clusters.

Writing a Cluster Spec
----------------------

As with all other Kubernetes configs, a Cluster needs `apiVersion`, `kind` and `metadata` fields. For general information about working with config files, see `using kubectl to manage resource <https://kubernetes.io/docs/concepts/overview/object-management-kubectl/overview/>`_.

A Cluster also needs a `.spec section <https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status>`_.

API Master
**********

``.spec.apiMaster`` is the URL of the Kubernetes cluster.

Capabilities
************

``.spec.capabilities`` is a required field that specifies the capabilities the cluster has.

Region
******

``.spec.region`` is a required field that specifies the region the cluster belongs to.

Scheduler
*********

``.spec.scheduler`` is an optional field that influences the scheduling evaluation for the cluster.

``.spec.scheduler.unschedulable`` is an optional field that if ``true`` the cluster is ignored during initial cluster selection.

``.spec.scheduler.weight`` is an optional field that assigns a weight to the cluster.

``.spec.scheduler.identity`` is an optional field that assigns an identity to the cluster different than its ``.metadata.name`` value.

Cluster Example
---------------

.. code-block:: yaml

    apiVersion: shipper.booking.com/v1
    kind: Cluster
    metadata:
      name: eu-ams-a
    spec:
      apiMaster: https://eu-ams-a.prod.example.com
      capabilities:
      - gpu
      - highBandwidth
      region: eu-ams
      scheduler:
        unschedulable: false
        weight: 0
        identity: "eu-ams-a"
    status:
      inService: true
