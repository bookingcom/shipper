.. _api-reference_capacity-target:

###############
Capacity Target
###############

A *CapacityTarget* is the interface used by the Release Controller to change
the target number of replicas for an application in a cluster. It is
acted upon by the Capacity Controller.

The ``status`` resource allows the Release Controller
to determine when the Capacity Controller is complete in a cluster and it can
move to the traffic step.

*******
Example
*******

.. literalinclude:: ../../examples/capacitytarget.yaml
    :language: yaml
    :linenos:

****
Spec
****

``.spec.percent``
=================

This field declares how much capacity the *Release* should have
in this cluster relative to the final
replica count. For example, if the final replica count is 10 and the
``percent`` is 50, the Deployment object for this *Release* will be patched to
have 5 pods.


``.spec.totalReplicaCount``
===========================

This field specifies the final replica count the *Release* should have in this cluster

******
Status
******

``.status.availableReplicas``
===========================

The number of pods that have successfully started up

``.status.achievedPercent``
===========================

This field shows the percentage of the final replica count does **availableReplicas**
represent.

``.status.conditions``
======================

A list of all conditions observed for this particular Application Cluster.

The following table displays the different conditions statuses and reasons reported in the
*CapacityTarget* object for the **Operational** condition type:

.. list-table::
    :widths: 1 1 1 99
    :header-rows: 1

    * - Type
      - Status
      - Reason
      - Description
    * - Operational
      - True
      - N/A
      - Cluster is reachable, and seems to be operational.
    * - Operational
      - False
      - ServerError
      - Some error has happened Shipper couldn't classify. Details can be
        found in the ``.message`` field.

The following table displays the different conditions statuses and reasons reported in the
*CapacityTarget* object for the **Ready** condition type:

.. list-table::
    :widths: 1 1 1 99
    :header-rows: 1

    * - Type
      - Status
      - Reason
      - Description
    * - Ready
      - True
      - N/A
      - The correct number of pods are running and all of them are Ready.
    * - Ready
      - False
      - WrongPodCount
      - This cluster has not yet achieved the desired number of pods.
    * - Ready
      - False
      - PodsNotReady
      - The cluster has the desired number of pods, but not all of them are
        Ready.
    * - Ready
      - False
      - MissingDeployment
      - Shipper could not find the Deployment object that it expects to be able
        to adjust capacity on. See ``message`` for more details.
