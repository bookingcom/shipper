.. _api-reference_application:

###########
Application
###########

An *Application* object represents a single application Shipper can manage on
a user's behalf. In this case, the term "application" means 'a collection of
Kubernetes objects installed by a single Helm chart'.

Application objects are a :ref:`user interface <user>`, and are the
primary way that application developers trigger new rollouts.

This is accomplished by editing an Application's ``.spec.template`` field. The
*template* field is a mold that Shipper will use to stamp out a new *Release*
object on each edit. This model is identical to to Kubernetes *Deployment*
objects and their ``.spec.template`` field, which serves as a mold for
*ReplicaSet* objects (and by extension, *Pod* objects).

Application's ``.spec.template.chart`` contains ambiguity by design: a user is
expected to provide either a specific chart version or a *SemVer constraint*
defining the range of acceptable chart versions. Shipper will resolve an
appropriate available chart version and pin the *Release* on it. Shipper
resolves the version in-place: it will substitute the initial constraint with a
specific resolved version and preserve the initial constraint in the Application
annotation named ``shipper.booking.com/app.chart.version.raw``.

The resolved ``.spec.template`` field will be copied to a new *Release*
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
of associated :ref:`Release <api-reference_release>` objects in ``.status.history``.

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

*Application*'s ``.spec.template.chart`` can define either a specific chart version,
or a SemVer constraint.

Please refer to `Semantic Version Ranges`_ section for more details on supported constraints.

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

***********************
Semantic Version Ranges
***********************

Shipper supports an extended range of semantic version constraints in
Application's ``.spec.template.chart.version``.

This section highlights the major features of supported SemVer constraints. For
a full reference please see `the underlying library spec`_.

.. _the underlying library spec: https://github.com/Masterminds/semver/blob/master/README.md

Composition
===========

SemVer specifications are composable: there are 2 composition operators defined:
- ``,``: stands for AND
- ``||``: stands for OR

In the example ``>=1.2.3, <3.4.5 || 6.7.8`` the constraint defines a range where
any version between 1.2.3 inclusive *and* 3.4.5 non-inclusive, *or* a specific
version 6.7.8 would satisfy it.

Trivial Comparisons
===================

Trivial comparison constraints belong to a category of equality check
relationships.

The range of comparison checks is defined as:
- ``=``: strictly equal to
- ``!=``: not equal to
- ``>``: greater than (non-inclusive)
- ``<``: less than (non-inclusive)
- ``>=``: greater than or equal to (inclusive)
- ``<=``: less than or equal to (inclusive)

The rest of the constraints is mainly a semantical syntax sugar and is fully
based on this category therefore the forecoming constraints are explained using
these operators.

Hyphens
=======

A hyphen-separated range is an equivalent to defining a lower and an upper
bound for a range of acceptable versions.

- ``1.2.3-4.5.6`` is equivalent to ``>=1.2.3, <=4.5.6``
- ``1.2-4.5`` is equivalent to ``>=1.2, <=4.5``

Wildcards
=========

There are 3 wildcard characters: ``x``, ``X`` and ``*``. They are absolutely
equivalent to each other: ``1.2.*`` is the same as ``1.2.X``.

- ``1.2.x`` is equivalent to ``>=1.2.0, <1.3.0`` (note the non-inclusive range)
- ``>=1.2.*`` is equivalent to ``>=1.2.0`` (the wildcard is optional here)
- ``*`` is equivalent to ``>=0.0.0`` (one can use ``x`` and ``X`` as well)

Tildes
======

A tilde is a context-dependant operator: it changes the range based on the
least significant version component provided.

- ``~1.2.3`` is equivalent to ``>=1.2.3, <1.3.0``
- ``~1.2`` is equivalent to ``>=1.2, <1.3``
- ``~1`` is equivalent to ``>=1, <2``

Carets
======

Carets pin the major version to a specific branch.

- ``^1.2.3`` is equivalent to ``>=1.2.3, <2.0.0``
- ``^1.2`` is equivalent to ``>=1.2, <2.0``

A caret-defined constraint is a handy way to say: give me the latest non-breaking
version.
