.. _api-reference_application:
###########
Application
###########

An *Application* object represents a single application Shipper can manage on
a user's behalf. In this case, the term "application" means 'a collection of
Kubernetes objects installed by a single Helm chart'.

Application objects are a :ref:`user interface <user_guide>`, and are the
primary way that application developers trigger new rollouts.

This is accomplished by editing an Application's ``.spec.template`` field. The
*template* field is a mold that Shipper will use to stamp out a new *Release*
object on each edit. This model is identical to to Kubernetes *Deployment*
objects and their ``.spec.template`` field, which serves as a mold for
*ReplicaSet* objects (and by extension, *Pod* objects).

The ``.spec.template`` field will be copied to a new *Release*
object under the ``.spec.environment`` field during deployment.

*******
Example
*******

.. literalinclude:: ../../examples/application.yaml
    :caption: Application example
    :language: yaml

****
Spec
****

``.spec.revisionHistoryLimit``
==============================

``revisionHistoryLimit`` is an optional field that represents the number
of associated :ref:`Release <concepts_release>` objects in ``.status.history``.

If you're using Shipper to configure development environments,
``revisionHistoryLimit`` can be a small value, like ``1``. In a production
setting it should be set to a larger number, like ``10`` or ``20``. This
ensures that you have plenty of rollback targets to choose from if something
goes wrong.

``.spec.template``
==================

The ``.spec.template`` is the only required field of the ``.spec``.

The ``.spec.template`` is a *Release* template. It has the same schema as the
:ref:`.spec.environment <api-reference_release_environment>` in a *Release*
object.

******
Status
******

``.status.history``
===================

``history`` is the sequence of *Releases* that belong to this *Application*.
This list is ordered by generation, old to new: the oldest *Release* is at the
start of the list, and the most recent (the **contender**) at the bottom.

``.status.conditions``
======================

All conditions contain five fields: ``lastTransitionTime``, ``status``, ``type``,
``reason``, and ``message``. Typically ``reason`` and ``message`` are omitted in the
expected case, and populated in the error or unexpected case.

``type: Aborting``
------------------

This condition indicates whether an abort is currently in progress. An abort is
when the latest *Release* (the **contender**) is deleted, triggering an
automatic rollback to the **incumbent**.

.. list-table::
    :widths: 1 1 1 99
    :header-rows: 1

    * - Type
      - Status
      - Reason
      - Description
    * - Aborting
      - True
      - N/A
      - The **contender** was deleted, triggering an abort. The *Application*
        ``.spec.template`` will be overwritten with the *Release*
        ``.spec.environment`` of the **incumbent**.
    * - Aborting
      - False
      - N/A
      - No abort is occurring.

``type: ReleaseSynced``
-----------------------

This condition indicates whether the **contender** *Release* reflects the
current state of the *Application* ``.spec.template``.

.. list-table::
    :widths: 1 1 1 99
    :header-rows: 1

    * - Type
      - Status
      - Reason
      - Description
    * - ReleaseSynced
      - True
      - N/A
      - Everything is OK: **Release** ``.spec.environment`` and **Application** ``.spec.template`` are in sync.
    * - ReleaseSynced
      - False
      - CreateReleaseFailed
      - The API call to Kubernetes to create the Release object failed. Check
        ``message`` for the specific error.

``type: RollingOut``
-----------------------

This condition indicates whether a rollout is currently in progress. A rollout
is in progress if the **contender** *Release* object has not yet achieved the
final step in the rollout strategy.

.. list-table::
    :widths: 1 1 1 99
    :header-rows: 1

    * - Type
      - Status
      - Reason
      - Description
    * - RollingOut
      - False
      - N/A
      - No rollout is in progress.
    * - RollingOut
      - True
      - N/A
      - A rollout is in progress. Check ``message`` for more details.

``type: ValidHistory``
-----------------------

This condition indicates whether the *Releases* listed in ``.status.history``
form a valid sequence.

.. list-table::
    :widths: 1 1 1 99
    :header-rows: 1

    * - Type
      - Status
      - Reason
      - Description
    * - ValidHistory
      - True
      - N/A
      - Everything is OK. All *Releases* have a valid generation annotation.
    * - ValidHistory
      - False
      - BrokenReleaseGeneration
      - One of the *Releases* does not have a valid generation annotation.
        Check ``message`` for more details.
    * - ValidHistory
      - False
      - BrokenApplicationObservedGeneration
      - The *Application* has an invalid ``highestObservedGeneration``
        annotation. check ``message`` for more details.
