.. _concepts_release:

Release
=======

A *Release* contains all the information required for Shipper to run a
particular version of an application.

To aid both the human and other users in finding resources related to a
particular *Release* object, the following labels are expected to be present
in a newly created *Release* and propagated to all of its related objects
(both in the **management** and **application** clusters):

shipper-app
    The name of the *Application* object owning the *Release*.

shipper-release
    The name of the *Release* object.



Scope of Control
----------------

*Release* objects are owned by *Application* objects through Kubernetes
`owner and dependents <https://kubernetes.io/docs/concepts/workloads/controllers/garbage-collection/#owners-and-dependents>`_
mechanism in the management cluster, and are created based on modifications
made to an application ``.spec.template`` field (read
:ref:`here <concepts_application_scope_of_control>` for more information about
an application's scope of control).

A *Release* object owns
*InstallationTarget*, *CapacityTarget* and *TrafficTarget* objects
through Kubernetes
`owner and dependents <https://kubernetes.io/docs/concepts/workloads/controllers/garbage-collection/#owners-and-dependents>`_
mechanism in the management cluster. Through Kubernetes garbage collection
mechanism, those objects are disposed when *Release* objects are removed from
the management cluster.

Writing a Release Spec
----------------------

.. _concepts_release_environment:

Release Environment
~~~~~~~~~~~~~~~~~~~

The ``.spec.environment`` is the only required field of the ``.spec``.

The ``.spec.environment`` contains all the information required for an
application to be deployed with Shipper.

.. important::
    *Roll-forwards* and *roll-backs* have no difference from Shipper's
    perspective, so a roll-back can be performed simply by replacing an
    Application's ``.spec.template`` field with the ``.spec.environment``
    field of the Release you want to roll-back to.

Chart
#####

.. literalinclude:: release-example.yaml
    :caption: Chart example
    :language: yaml
    :lines: 7-10
    :dedent: 4
    :linenos:

``.spec.environment.chart`` is a required field that specifies a chart to be rendered by Shipper when installing the Release.

``.spec.environment.chart.name`` is a required field that specifies the name of the chart to be rendered.

``.spec.environment.chart.version`` is a required field that specifies the chart version to be rendered.

``.spec.environment.chart.repoUrl`` is a required field that specifies the Helm chart repository where the find the chart to be rendered.


Cluster Requirements
####################

.. literalinclude:: release-example.yaml
    :caption: Cluster requirements example
    :language: yaml
    :lines: 11-17
    :dedent: 4
    :linenos:

``.spec.environment.clusterRequirements`` is a required field that specifies cluster regions and capabilities to be able to host the application.

``.spec.environment.clusterRequirements.regions`` is a required field that specifies region requirements for a cluster to be able to host the application.

Strategy
########

.. literalinclude:: release-example.yaml
    :caption: Vanguard strategy example
    :language: yaml
    :lines: 18-40
    :dedent: 4
    :linenos:

``.spec.environment.strategy`` is a required field that specifies the
deployment strategy to be used when deploying the release.

``.spec.environment.strategy.steps`` contains a list of steps that must
be executed in order to complete a release. The following table displays
the keys a step entry should have:

.. list-table::
    :widths: 1 99
    :header-rows: 1

    * - Key
      - Description

    * - ``.name``
      - The step name, meant for human users. For example, ``staging``, ``50/50`` or ``full on``.

    * - ``.capacity.incumbent``
      - The percentage of replicas, from the total number of required replicas
        the **incumbent Release** should have at this step.

    * - ``.capacity.contender``
      - The percentage of replicas, from the total number of required replicas
        the **contender Release** should have at this step.

    * - ``.traffic.incumbent``
      - The weight the **incumbent Release** has when load balancing traffic
        through all Release objects of the given Application.

    * - ``.traffic.contender``
      - The weight the **contender Release** has when load balancing traffic
        through all Release objects of the given Application.

Values
######

``.spec.environment.values`` is an optional field that specifies values to be informed when rendering the release's chart.

Release Example
---------------

.. literalinclude:: release-example.yaml
    :caption: Release example
    :language: yaml
    :linenos:
