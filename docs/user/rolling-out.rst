.. _user_rolling-out:

########################
Rolling out with Shipper
########################

Rollouts with Shipper are all about transitioning from an old *Release*, the
**incumbent**, to a new *Release*, the **contender**. If you're rolling out
an *Application* for the very first time, then there is no **incumbent**, only
a **contender**.

In general Shipper tries to present a familiar interface for people accustomed
to *Deployment* objects.

******************
Application object
******************

Here's the Application object we'll use:

.. code-block:: yaml

  apiVersion: shipper.booking.com/v1alpha1
  kind: Application
  metadata:
    name: super-server
  spec:
    revisionHistoryLimit: 3
    template:
      chart:
        name: nginx
        repoUrl: https://storage.googleapis.com/shipper-demo
        version: 0.0.1
      clusterRequirements:
        regions:
        - name: local
      strategy:
        steps:
        - capacity:
            contender: 1
            incumbent: 100
          name: staging
          traffic:
            contender: 0
            incumbent: 100
        - capacity:
            contender: 100
            incumbent: 0
          name: full on
          traffic:
            contender: 100
            incumbent: 0
      values:
        replicaCount: 3

Copy this to a file called ``app.yaml`` and apply it to our Kubernetes cluster:

.. code-block:: shell

    $ kubectl apply -f app.yaml

This will create an *Application* and *Release* object. Shortly thereafter, you
should also see the set of Chart objects: a *Deployment*, a *Service*, and
a *Pod*.

*****************
Checking progress
*****************

There are a few different ways to figure out how your rollout is going.

We can check in on the *Release* to see what kind of progress we're making:

``.status.achievedStep``
------------------------

This field is the definitive answer for whether Shipper considers a given step in
a rollout strategy complete.

.. code-block:: shell

	$ kubectl get rel super-server-83e4eedd-0 -o json | jq .status.achievedStep
	null
	$ # "null" means Shipper has not written the achievedStep key, because it hasn't finished the first step
	$ kubectl get rel -o json | jq .items[0].status.achievedStep
	{
	  "name": "staging",
	  "step": 0
	}

If everything is working, you should see one *Pod* active/ready. 

``.status.strategy.conditions``
-------------------------------

For a more detailed view of what's happening while things are in between
states, you can use the Strategy conditions.

.. code-block:: shell
    
    $ kubectl get rel super-server-83e4eedd-0 -o json | jq .status.strategy.conditions
    [
      {
        "lastTransitionTime": "2018-12-09T10:00:55Z",
        "message": "clusters pending capacity adjustments: [microk8s]",
        "reason": "ClustersNotReady",
        "status": "False",
        "type": "ContenderAchievedCapacity"
      },
      {
        "lastTransitionTime": "2018-12-09T10:00:55Z",
        "status": "True",
        "type": "ContenderAchievedInstallation"
      }
    ]

These will tell you which part of the step Shipper is currently working on. In
this example, Shipper is waiting for the desired capacity in the microk8s
cluster. This means that Pods aren't ready yet.

``.status.strategy.state``
--------------------------

Finally, because the Strategy conditions can be kind of a lot to parse, they
are summarized into ``estatus.strategy.state``.
 
.. code-block:: shell

	$ kubectl get rel super-server-83e4eedd-0 -o json | jq .status.strategy.state
	{
	  "waitingForCapacity": "True",
	  "waitingForCommand": "False",
	  "waitingForInstallation": "False",
	  "waitingForTraffic": "False"
	}

The :ref:`troubleshooting guide <user_troubleshooting>` has more information on
how to dig deep into what's going on with any given *Release*.

*********************
Advancing the rollout
*********************

So now that we've checked on our *Release* and seen that Shipper considers step
0 achieved, let's advance the rollout:

.. code-block:: shell

    $ kubectl patch rel super-server-83e4eedd-0 --type=merge -p '{"spec":{"targetStep":1}}'

I'm using ``patch`` here to keep things concise, but any means of modifying
objects will work just fine.

Now we should be able to see 2 more pods spin up:

.. code-block:: shell

    $ kubectl get po
    NAME                                             READY STATUS  RESTARTS AGE
    super-server-83e4eedd-0-nginx-5775885bf6-76l6g   1/1   Running 0        7s
    super-server-83e4eedd-0-nginx-5775885bf6-9hdn5   1/1   Running 0        7s
    super-server-83e4eedd-0-nginx-5775885bf6-dkqbh   1/1   Running 0        3m55s

And confirm that Shipper believes this rollout to be done:

.. code-block:: shell

	$ kubectl get rel -o json | jq .items[0].status.achievedStep
	{
	  "name": "full on",
	  "step": 1
	}

That's it! Doing another rollout is as simple as editing the *Application*
object, just like you would with a *Deployment*. The main principle is
patching the *Release* object to move from step to step.

#################
Blocking rollouts
#################

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

Copy this to a file called ``globalRolloutBlock.yaml`` and apply it to our Kubernetes cluster:

.. code-block:: shell

    $ kubectl apply -f globalRolloutBlock.yaml

This will create a Global *RolloutBlock* object.
In order to create a namespace rollout block, simply state the relevant namespace in the yaml file.

While this object is in the system, there can not be any change to the `.Spec` of any object. Shipper
will reject the creation of new objects and patching of existing releases.

**************************
Overriding a rollout block
**************************

Rollout blocks can be overriden with an annotation applied to the *Application* or *Release* object which
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
      shipper.booking.com/block.override: rollout-blocks-global/dns-outage
  spec:
    revisionHistoryLimit: 3
    template:
      # ... rest of template omitted here

The annotation may reference multiple blocks:

.. code-block:: yaml

    shipper.booking.com/block.override: rollout-blocks-global/dns-outage,frontend/demo-to-investors-in-progress

The block override annotation format is CSV.

The override annotation **must** reference specific, fully-qualified *RolloutBlock* objects by name.
Non-existing blocks enlisted in this annotation are not allowed.
If there exists a Release object for a specific application, the release should be the one overriding.

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
      - type: RolloutBlock
        status: True
        reason: rollout-blocks-global/dns-outage

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

.. code-block::
    NAMESPACE               NAME        MESSAGE                                   AUTHOR TYPE   AUTHOR NAME   OVERRIDING APPLICATIONS   OVERRIDING RELEASES
    rollout-blocks-global   dns-outage  DNS issues, troubleshooting in progress   user          jdoe          default/super-server      default/super-server-83e4eedd-0
