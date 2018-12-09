.. _api-reference_release:

#######
Release
#######

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

*******
Example
*******

.. literalinclude:: ../../examples/release.yaml
    :language: yaml
    :linenos:

****
Spec
****

``.spec.targetStep``
====================

**targetStep** defines which strategy step this *Release* should be trying to
complete. It is the primary interface for users to advance or retreat a given
rollout.

.. _api-reference_release_environment:

``.spec.environment``
=====================

The **environment** contains all the information required for an
application to be deployed with Shipper.

.. important::
    *Roll-forwards* and *roll-backs* have no difference from Shipper's
    perspective, so a roll-back can be performed simply by replacing an
    Application's ``.spec.template`` field with the ``.spec.environment``
    field of the Release you want to roll-back to.

``.spec.environment.chart``
---------------------------

.. literalinclude:: ../../examples/release.yaml
    :language: yaml
    :lines: 8-11
    :linenos:

The environment **chart** key defines the Helm Chart that contains the Kubernetes object
templates for this *Release*. ``name``, ``version``, and ``repoUrl`` are all
required. ``repoUrl`` is the Helm Chart repository that Shipper should
download the chart from.

.. note::

    Shipper will cache this chart version internally after fetching it, just
    like ``pullPolicy: IfNotPresent`` for Docker images in Kubernetes. This
    protects against chart repository outages. However, it means that if you
    need to change your chart, you need to tag it with a different version.

``.spec.environment.clusterRequirements``
-----------------------------------------

.. literalinclude:: ../../examples/release.yaml
    :language: yaml
    :lines: 12-17
    :linenos:

The environment **clusterRequirements** key specifies what kinds of clusters
this *Release* can be scheduled to. It is required.

``clusterRequirements.capabilities`` is a list of capability names this
*Release* requires. They should match capabilities specified in :ref:`Cluster
<api-reference_cluster_capabilities>` objects exactly. This may be left empty
if the *Release* has no required capabilities.

``clusterRequirements.regions`` is a list of regions this *Release* must run in. It is required.

``.spec.environment.strategy``
------------------------------

.. literalinclude:: ../../examples/release.yaml
    :language: yaml
    :lines: 18-40
    :linenos:

The environment **strategy** is a required field that specifies the rollout strategy to
be used when deploying the *Release*.

``.spec.environment.strategy.steps`` contains a list of steps that must be
executed in order to complete a release. A step should have the follwing keys:

.. list-table::
    :widths: 1 99
    :header-rows: 1

    * - Key
      - Description

    * - ``.name``
      - The step name, meant for human users. For example, ``staging``, ``canary`` or ``full on``.

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

``.spec.environment.values``
----------------------------

The environment **values** key provides parameters for the Helm Chart templates. It is
exactly equivalent to a ``values.yaml`` file provided to the ``helm install -f
values.yaml`` invocation. Like ``values.yaml`` it is technically optional, but
almost all rollouts are likely to include some dynamic values for the chart,
like the image tag.

Almost all Charts will expect some **values** like ``replicaCount``,
``image.repository``, and ``image.tag``.

******
Status
******

``.status.achievedStep``
========================

**achievedStep** indicates which strategy step was most recently completed.

``.status.conditions``
======================

All conditions contain five fields: ``lastTransitionTime``, ``status``, ``type``,
``reason``, and ``message``. Typically ``reason`` and ``message`` are omitted in the
expected case, and populated in the error or unexpected case.

``type: Complete``
------------------

This condition indicates whether a *Release* has finished its strategy, and
should be considered complete.

``type: Scheduled``
-------------------

This condition indicates whether the ``clusterRequirements`` were satisfied and
a concrete set of clusters selected for this *Release*.

``.status.strategy``
====================

This section contains information on the progression of the strategy.

``.status.strategy.conditions``
-------------------------------

These conditions represent the precise state of the strategy: for each of the
**incumbent** and **contender**, whether they have converged on the state
defined by the given strategy step.

``.status.strategy.state``
--------------------------

The **state** keys are intended to make it easier to interpret the strategy
conditions by summarizing into a high level conclusion: what is Shipper waiting
for right now? If it is ``waitingForCommand: "True"`` then the rollout is
awaiting a change to ``.spec.targetStep`` to proceed. If any other key is
``True``, then Shipper is still working to achieve the desired state.
