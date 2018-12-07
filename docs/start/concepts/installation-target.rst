.. _concept_installation_target:

Installation Target
===================

An *InstallationTarget* describes the concrete set of clusters where the release
should be installed. It is created by the Schedule Controller after the
concrete clusters are picked using ``clusterRequirements``.

The Installation Controller acts on InstallationTarget objects by getting the
chart, values, and sidecars from the associated Release object,
rendering the chart per-cluster, and inserting those objects into each target
cluster. Where applicable, these objects are always created with 0 replicas.

It updates the ``status`` resource to indicate progress for each target cluster.

Since the definition of a desired state heavily depends on the strategy
implemented by the Strategy Controller, as in some strategies being more lenient
than other with respect to the installation process, a global status for
InstallationTarget was omitted.

Scope of Control
----------------

*InstallationTarget* objects are owned by *Release* objects through Kubernetes
`owner and dependents <https://kubernetes.io/docs/concepts/workloads/controllers/garbage-collection/#owners-and-dependents>`_
mechanism in the management cluster, and are created after the associated
*Release* object is updated with the list of clusters where it must be
installed.

An *InstallationTarget* doesn't own any other object on the management cluster,
but it **does** own a special object on all Application Clusters the Release has
been installed, which we call *ConfigMap Anchor*.

The ConfigMap Anchor is assigned as owner (through the same Kubernetes owner
and dependents mechanism mentioned above) of *all objects related to the
associated Release* in an Application Cluster. The only data currently being
stored in this ConfigMap Anchor is the related InstallationTarget's UUID to
establish a relationship between both objects.

Removing a Release's ConfigMap Anchor from an Application Cluster removes all
related objects through Kubernetes garbage collection mechanism. If an Anchor
has been removed, but the related Release hasn't, Shipper will re-install
all Release objects.

When a *Release* object is removed from the management cluster, the
*InstallationTarget* is also removed through Kubernetes garbage collection
mechanism. Objects associated with the deleted Release are evicted from
Application Clusters by Shipper through its internal garbage collection
system (Shipper's Janitor Controller).

Writing an InstallationTarget Spec
----------------------------------

The ``.spec.clusters`` field is a list of cluster names known to Shipper (read
more about clusters :ref:`here <concept_cluster>`) the associated *Release*
should be installed.

.. literalinclude:: installation-target-example.yaml
    :caption: Clusters example
    :language: yaml
    :lines: 8-11
    :dedent: 2
    :linenos:

Reading an InstallationTarget Status
------------------------------------

The ``.status.clusters`` field is a list of objects representing the
installation status of all clusters where the associated Release objects
must be installed.

.. literalinclude:: installation-target-example.yaml
    :caption: Clusters example
    :language: yaml
    :lines: 13-23
    :dedent: 2
    :linenos:

The following table displays the keys a cluster status entry should have:

.. list-table::
    :widths: 1 99
    :header-rows: 1

    * - Key
      - Description
    * - **name**
      - The Application Cluster name. For example, **eu-ams-a**.
    * - **status**
      - **Failed** in case of failure, or **Installed** in case of success.
    * - **message**
      - A message describing the reason Shipper decided that it has failed.
    * - **conditions**
      - A list of all conditions observed for this particular Application Cluster.

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

InstallationTarget Example
--------------------------

.. literalinclude:: installation-target-example.yaml
    :language: yaml
    :linenos:
