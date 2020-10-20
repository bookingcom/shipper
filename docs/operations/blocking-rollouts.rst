.. _operations_blocking-rollouts:

Blocking rollouts
=================

You can block rollouts in a specific namespace, or all namespaces (if you have the permissions to do
so). To do so, you simply create a *RolloutBlock* object. The *RolloutBlock* object represents a
rollout block in a specific namespace. When the object is deleted, the block is lifted.

*******************
RolloutBlock object
*******************

Here's an example for a RolloutBlock object we'll use:

.. code-block:: yaml

    apiVersion: shipper.booking.com/v1alpha1
    kind: RolloutBlock
    metadata:
      name: dns-outage
      namespace: rollout-blocks-global # for global rollout block. for a local one use the correct namespace.
    spec:
      message: DNS issues, troubleshooting in progress
      author:
        type: user
        name: jdoe # This indicates that a rollout block was put in place by user 'jdoe'

Copy this to a file called ``globalRolloutBlock.yaml`` and apply it to your Kubernetes cluster:

.. code-block:: shell

    $ kubectl apply -f globalRolloutBlock.yaml

This will create a *Global RolloutBlock* object.
In order to create a namespace rollout block, simply state the relevant namespace in the yaml file.
An example for a namespaced RolloutBlock object:

.. code-block:: yaml

    apiVersion: shipper.booking.com/v1alpha1
    kind: RolloutBlock
    metadata:
      name: fairy-investigation
      namespace: fairytale-land
    spec:
      message: Investigating current Fairy state
      author:
        type: user
        name: fgodmother


While this object is in the system, there can not be any change to the `.Spec` of any object. Shipper
will reject the creation of new objects and patching of existing releases.

**************************
Overriding a rollout block
**************************

Rollout blocks can be overridden with an annotation applied to the *Application* or *Release* object which
needs to bypass the block. This annotation will list each RolloutBlock object that it overrides with
a fully-qualified name (namespace + name).

For example, mending our Application object to override
the global rollout block that we set in place:

.. code-block:: yaml

  apiVersion: shipper.booking.com/v1alpha1
  kind: Application
  metadata:
    name: super-server
    annotations:
      shipper.booking.com/rollout-block.override: rollout-blocks-global/dns-outage
  spec:
    revisionHistoryLimit: 3
    template:
      # ... rest of template omitted here

The annotation may reference multiple blocks:

.. code-block:: yaml

    shipper.booking.com/rollout-block.override: rollout-blocks-global/dns-outage,frontend/demo-to-investors-in-progress

The block override annotation format is CSV.

The override annotation **must** reference specific, fully-qualified *RolloutBlock* objects by name.
Non-existing blocks enlisted in this annotation are not allowed.
If there exists a Release object for a specific application, the release should be the one overriding it.

**********************************
Application and Release conditions
**********************************

Application and Release objects will have a `.status.conditions` entry which lists all of the
blocks which are currently in effect.

For example:

.. code-block:: yaml

    apiVersion: shipper.booking.com/v1
    kind: Application
    metadata:
      name: ui
      namespace: frontend
    spec:
      # ... spec omitted
    status:
      conditions:
      - type: Blocked
        status: True
        reason: RolloutsBlocked
        message: rollouts blocked by: rollout-blocks-global/dns-outage

This will be accompanied with an event (can be viewed with ``kubectl describe application ui -n frontend``).
For example:

.. code-block:: yaml

    Events:
      Type     Reason             Age                 From                    Message
      ----     ------             ----                ----                    -------
      Warning  RolloutBlock       3s (x3 over 5s)     application-controller  rollout-blocks-global/dns-outage

*******************************
Checking a rollout block status
*******************************

There are a few simple ways to know which objects are overriding your RolloutBlock object.

``.status.overrides``
---------------------

This fields will state all living Application and Release objects that override this RolloutBlock object.

.. code-block:: shell

    $ kubectl -n rollout-blocks-global get rb dns-outage -o yaml

This might look like this:

.. code-block:: yaml

    apiVersion: shipper.booking.com/v1alpha1
    kind: RolloutBlock
    metadata:
      name: dns-outage
      namespace: rollout-blocks-global
    # ... spec omitted
    status:
      # associated because 'shipper-system/dns-outage' is referenced in override annotation
      overrides:
        applications: default/super-server
        release: default/super-server-83e4eedd-0

``output wide``
---------------

This will show all information about all rollout blocks in the namsespace (default if not specify,
`rollout-blocks-global` for all global RolloutBlocks ,`--all-namespaces` for all rollout blocks)

.. code-block:: shell

    $ kubectl -n rollout-blocks-global get rb -o wide

This might look like this:

.. code-block:: text

    NAMESPACE               NAME        MESSAGE                                   AUTHOR TYPE   AUTHOR NAME   OVERRIDING APPLICATIONS   OVERRIDING RELEASES
    rollout-blocks-global   dns-outage  DNS issues, troubleshooting in progress   user          jdoe          default/super-server      default/super-server-83e4eedd-0
