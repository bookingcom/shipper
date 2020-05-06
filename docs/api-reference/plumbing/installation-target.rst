.. _api-reference_low-level_installation-target:

###################
Installation Target
###################

An *InstallationTarget* describes the concrete set of clusters where the release
should be installed, and lives in these clusters. It is created by the Release Controller's Scheduler
after the concrete clusters are picked using ``clusterRequirements``.

The Installation Controller acts on InstallationTarget objects by getting the
chart, values, and sidecars from the associated Release object,
rendering the chart in-cluster, and inserting those objects into the
cluster. Where applicable, these objects are always created with 0 replicas.

It updates the ``status`` resource to indicate progress in the target cluster.

*******
Example
*******

.. literalinclude:: ../../examples/installationtarget.yaml
    :language: yaml
    :linenos:

****
Spec
****

``.spec.canOverride``
====================

This field allows the installation target to override objects that are not owned by it.

``.spec.chart`` ``.spec.values``
================================

These fields are copied from the :ref:`Release object <api-reference_release_environment>`.

******
Status
******

``.status.conditions``
===============================

A list of all conditions observed for this particular Application Cluster.

The following table displays the different conditions statuses and reasons reported in the
*InstallationTarget* object for the **Operational** condition type:

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
      - TargetClusterClientError
      - There is a problem contacting the Application Cluster; Shipper
        either doesn't know about this Application Cluster, or there is
        another issue when accessing the Application Cluster. Details
        can be found in the ``.message`` field.
    * - Operational
      - False
      - ServerError
      - Some error has happened Shipper couldn't classify. Details can be
        found in the ``.message`` field.

The following table displays the different conditions statuses and reasons reported in the
*InstallationTarget* object for the **Ready** condition type:

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
      - Indicates that Kubernetes has achieved the desired state related to
        the *InstallationTarget* object.
    * - Ready
      - False
      - ServerError
      - Shipper could not either create an object in the Application Cluster,
        or an error occurred when trying to fetch an object from the
        Application Cluster. Details can be found in the ``.message`` field.
    * - Ready
      - False
      - ChartError
      - There was an issue while processing a Helm Chart, such as invalid
        templates being used as input, or rendered templates that do not
        match any known Kubernetes object. Details can be found in the
        ``.message`` field.
    * - Ready
      - False
      - ClientError
      - Shipper couldn't create a resource client to process a particular
        rendered object. Details can be found in the ``.message`` field.
    * - Ready
      - False
      - UnknownError
      - Some error Shipper couldn't classify has happened. Details can be
        found in the ``.message`` field.
