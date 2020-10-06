.. _api-reference_traffic-target:

###################
Traffic Target
###################

A *TrafficTarget* is an interface to a method of shifting traffic between
different *Releases* based on weight. This may be implemented in a number of
ways: pod labels and Service objects, service mesh manipulation, or something
else. For the moment only vanilla Kubernetes traffic shifting is supported: pod
labels and Service objects.

It is manipulated by the Strategy Controller as part of executing a release
strategy.

*******
Example
*******

.. literalinclude:: ../../examples/traffictarget.yaml
    :language: yaml
    :linenos:

****
Spec
****

``.spec.clusters``
====================

.. literalinclude:: ../../examples/traffictarget.yaml
    :language: yaml
    :lines: 6-11
    :linenos:

``clusters`` is a list of cluster entries and the desired traffic weight for
this *Release* in that cluster. The Traffic controller calculates the correct
traffic ratio for this *Release* by summing weights from all *TrafficTarget*
objects available.

******
Status
******

``.status.clusters``
====================

``.status.clusters`` is a list of objects representing the traffic status
of all clusters where the associated Release objects must be installed.

.. literalinclude:: ../../examples/traffictarget.yaml
    :language: yaml
    :lines: 12-
    :linenos:

The following table displays the keys a cluster status entry should have:

.. list-table::
    :widths: 1 99
    :header-rows: 1

    * - Key
      - Description
    * - **name**
      - The Application Cluster name. For example, **kube-us-east1-a**.
    * - **status**
      - **Failed** in case of failure, or **Synced** in case of success.
    * - **achievedTraffic**
      - The traffic weight achieved by Shipper for this cluster.
    * - **conditions**
      - A list of all conditions observed for this particular Application Cluster.

``.status.clusters.conditions``
===============================

The following table displays the different conditions statuses and reasons reported in the
*TrafficTarget* object for the **Operational** condition type:

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
      - There is a problem contacting the Application Cluster; Shipper
        either doesn't know about this Application Cluster, or there is
        another issue when accessing the Application Cluster. Details
        can be found in the ``.message`` field.

The following table displays the different conditions statuses and reasons reported in the
*TrafficTarget* object for the **Ready** condition type:

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
      - The desired traffic weight has been successfully achieved.
    * - Ready
      - False
      - MissingService
      - Shipper could not find a Service object to use for traffic shifting.
        Check ``message`` for more details.
    * - Ready
      - False
      - ServerError
      - Shipper got an error status code while calling the Kubernetes API of
        the Application Cluster. Details in the ``.message`` field.
    * - Ready
      - False
      - ClientError
      - Shipper couldn't create a resource client to process a particular
        rendered object. Details can be found in the ``.message`` field.
    * - Ready
      - False
      - InternalError
      - Something went wrong with the math that Shipper does to calculate the
        desired number of pods. See the ``.message`` field for the exact error.
    * - Ready
      - False
      - UnknownError
      - Some error Shipper couldn't classify has happened. Details can be
        found in the ``.message`` field.
