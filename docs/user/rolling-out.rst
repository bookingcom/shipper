.. _user_rolling-out:

########################
Rolling out with Shipper
########################

.. note::

   This documentation assumes that you have set up Shipper in two
   clusters. ``kind-mgmt`` is the name of the context that points to
   the *management* cluster, and ``kind-app`` is the name of the
   context that points to the *application* cluster.


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
        repoUrl: https://raw.githubusercontent.com/bookingcom/shipper/master/test/e2e/testdata
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

Copy this to a file called ``app.yaml`` and apply it to your Kubernetes management cluster:

.. code-block:: shell

    $ kubectl --context kind-mgmt apply -f app.yaml

This will create an *Application* and *Release* object. Shortly thereafter, you
should also see the set of Chart objects: a *Deployment*, a *Service*, and
a *Pod*.

*****************
Checking progress
*****************

There are a few different ways to figure out how your rollout is going.

We can check in on the *Release* to see the progress we're making:

``.status.achievedStep``
------------------------

This field is the definitive answer for whether Shipper considers a given step in
a rollout strategy complete.

.. code-block:: shell

	$ kubectl --context kind-mgmt get rel super-server-83e4eedd-0 -o json | jq .status.achievedStep
	null
	$ # "null" means Shipper has not written the achievedStep key, because it hasn't finished the first step
	$ kubectl get rel -o json | jq .items[0].status.achievedStep
	{
	  "name": "staging",
	  "step": 0
	}

If everything is working, you should see one *Pod* active/ready. 

``.status.conditions``
----------------------

Just like any other object, the ``status`` field of a *Release* object
contains information on anything that is going wrong, and anything that is going right:

.. code-block:: shell
   $ kubectl --context kind-mgmt get rel super-server-83e4eedd-0 -o json | jq .status.conditions
   [
     {
       "lastTransitionTime": "2020-06-04T09:44:39Z",
       "status": "False",
       "type": "Blocked"
     },
     {
       "lastTransitionTime": "2020-06-04T09:44:39Z",
       "message": "kind-app",
       "reason": "ClustersChosen",
       "status": "True",
       "type": "ClustersChosen"
     },
     {
       "lastTransitionTime": "2020-06-04T12:08:05Z",
       "message": "cluster \"kind-app\" not ready for use yet; cluster client is being initialized",
       "reason": "StrategyExecutionFailed",
       "status": "False",
       "type": "StrategyExecuted"
     }
   ]

This set of conditions shows that the strategy hasn't been executed
because Shipper can not contact the *application* cluster called
``kind-app``.

``.status.strategy.conditions``
-------------------------------

For a more detailed view of what's happening while things are in between
states, you can use the Strategy conditions.

.. code-block:: shell
    
    $ kubectl --context kind-mgmt get rel super-server-83e4eedd-0 -o json | jq .status.strategy.conditions
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

    $ kubectl --context kind-mgmt patch rel super-server-83e4eedd-0 --type=merge -p '{"spec":{"targetStep":1}}'

I'm using ``patch`` here to keep things concise, but any means of modifying
objects will work just fine.

Now, if you've got your ``kind-app`` context set to the same namespace as your
**Application** object in the  *management* cluster, you should be able to see 2 more pods spin up:

.. code-block:: shell

    $ kubectl --context kind-app get po
    NAME                                             READY STATUS  RESTARTS AGE
    super-server-83e4eedd-0-nginx-5775885bf6-76l6g   1/1   Running 0        7s
    super-server-83e4eedd-0-nginx-5775885bf6-9hdn5   1/1   Running 0        7s
    super-server-83e4eedd-0-nginx-5775885bf6-dkqbh   1/1   Running 0        3m55s

And confirm that Shipper believes this rollout to be done:

.. code-block:: shell

	$ kubectl --context kind-mgmt get rel -o json | jq .items[0].status.achievedStep
	{
	  "name": "full on",
	  "step": 1
	}

That's it! Doing another rollout is as simple as editing the *Application*
object, just like you would with a *Deployment*. The main principle is
patching the *Release* object to move from step to step.
