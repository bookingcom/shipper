.. _api-reference_capacity-target:

###############
Capacity Target
###############

A *CapacityTarget* is the interface used by the Strategy Controller to change
the target number of replicas for an application in a set of clusters. It is
acted upon by the Capacity Controller.

The ``status`` resource includes status per-cluster so that the Strategy
Controller can determine when the Capacity Controller is complete and it can
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

``.spec.clusters``
===================

``clusters`` is a list of clusters the associated *Release* object is present
in. Each item in the list has a ``name``, which should map to a :ref:`Cluster
<api-reference_cluster>` object, and a ``percent``. ``percent`` declares how
much capacity the *Release* should have in this cluster relative to the final
replica count. For example, if the final replica count is 10 and the
``percent`` is 50, the Deployment object for this *Release* will be patched to
have 5 pods.

.. literalinclude:: ../../examples/capacitytarget.yaml
    :language: yaml
    :lines: 9-14
    :linenos:

******
Status
******

``.status.clusters``
====================

``.status.clusters`` is a list of objects representing the capacity status
of all clusters where the associated Release objects must be installed.

.. literalinclude:: ../../examples/capacitytarget.yaml
    :language: yaml
    :lines: 15-
    :linenos:

The following table displays the keys a cluster status entry should have:

.. list-table::
    :widths: 1 99
    :header-rows: 1

    * - Key
      - Description
    * - **name**
      - The Application Cluster name. For example, **kube-us-east1-a**.
    * - **availableReplicas**
      - The number of pods that have successfully started up
    * - **achievedPercent**
      - What percentage of the final replica count does **availableReplicas**
        represent.
    * - **sadPods**
      - Pod Statuses for up to 5 Pods which are not yet Ready.
    * - **conditions**
      - A list of all conditions observed for this particular Application Cluster.

``.status.clusters.conditions``
===============================

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
