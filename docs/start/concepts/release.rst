.. _concepts_release:

Release
=======

A *Release* encodes all of the information required to rollout (or rollback) a
particular version of an application. It represents the transition from the
incumbent version to the candidate version.

It consists primarily of an immutable ``.metadata.environment`` from the
Application ``template``. This environment is annotated by the Schedule
controller to transform ``clusterRequirements`` into concrete clusters; it may
also contain mandatory sidecars or anything else required to install or upgrade
the application in a cluster.

Additionally, its ``spec`` is the interface used by the outside world to indicate
that it is safe to proceed with a release strategy.

Its ``status`` sub-object is used by various controllers to encode the position
of this operation in the overall release state machine.

The *Phase* field in the Status object indicates the deployment/release phase
the system is waiting for completion, and it is up to the aforementioned various
controllers to determine whether there is work to be done, and also is their
responsibilities to modify the field to activate the next phase.

The ``release`` label is used to further identify all the Kubernetes objects
that were created by this particular release.

Writing a Release Spec
----------------------

.. _concepts_release_environment:

Release Environment
~~~~~~~~~~~~~~~~~~~

The ``.spec.environment`` is the only required field of the ``.spec``.

The ``.spec.environment`` contains all the information required for an application to be deployed with Shipper.

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

``.spec.environment.strategy`` is a required field that specifies the deployment strategy to be used when deploying the release.

Values
######

``.spec.environment.values`` is an optional field that specifies values to be informed when rendering the release's chart.

Release Example
---------------

.. literalinclude:: release-example.yaml
    :caption: Release example
    :language: yaml
    :linenos:
