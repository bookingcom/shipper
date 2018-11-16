.. _concept_cluster:

Cluster
=======

A *Cluster* object contains target K8s cluster capabilities and status. This
information is used by the Schedule Controller to resolve the cluster selectors.
It is also used by the Installation, Capacity, and Traffic controllers to locate
the API masters.

Cluster Example
---------------

.. code-block:: yaml

    apiVersion: shipper.booking.com/v1
    kind: Cluster
    spec:
      name: eu-ams-a
      region: eu-ams
      apiMaster: https://eu-ams-a-k8s.prod.booking.com
      schedulable: true
      capabilities:
      - gpu
      - highBandwidth
    status:
      availableResources:
        memory: 3216gb
        ipv4Addresses: 68
        cpu: 2749 cores
