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

Copy this to a file called ``app.yaml`` and apply it to our Kubernetes cluster:

.. code-block:: shell

    $ kubectl apply -f app.yaml

This will create an *Application* and *Release* object. Shortly thereafter, you
should also see the set of Chart objects: a *Deployment*, a *Service*, and
a *Pod*.

For more information about the Application object, see :ref:`Rolling out strategy`


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

.. _Rolling out strategy:

####################
Rolling out strategy
####################

The ``spec`` section lets Shipper know what is the rolling update strategy.

**************
Specifications
**************

``.spec.revisionHistoryLimit`` is an optional field, defining the number of old Release objects to retain, in order to allow rollback. The default value is 20.

``.spec.template`` is a Release template. This is a required field of the ``.spec``.
Every time you change the template for your Application object, Shipper will create a Release object and perform a rollout.

Cluster Requirements
--------------------

| ``.spec.template.clusterRequirements`` defines the clusters where our application should be deployed.
| ``.spec.template.clusterRequirements.regions`` is a required field. It defines the regions of the clusters Shipper can select from to deploy the application to.
|   ``name`` is the region name of the cluster.
|   ``replicas``  is an optional field that specifies how many clusters your application should be scheduled on in this region. The default value is 1.
| ``.spec.template.clusterRequirements.capabilities`` is an optional field, defines the required capabilities a cluster must have in order for Shipper to select it for deployment.
| Look at the Cluster objects to see what regions and capabilities you can specify.

Chart
-----

| ``.spec.template.chart`` defines the `Chart <https://helm.sh/docs/>`_ to be used.
|   ``name``: Chart's name.
|   ``version``: Chart's version. This can be either the specific desired version, e.g. 0.2.2 or a `semantic version <https://semver.org/>`_, e.g. "2^".
|   ``repoUrl``: `Chart repository <https://helm.sh/docs/topics/chart_repository/>`_ to retrieve the chart from.
Shipper will cache this chart version internally after fetching to protect against repository outages. This means that if you need to change your chart, you need to tag it with a different version.

Values
------

| Inside the template section ``values`` apply to the defined Chart.
| ``values`` are what the chart templates get as parameters when they render. Some of these will likely change every rollout, for example updating the tag of the image you want to deploy.
| These are specific to the Chart you are deploying.
Here are some useful keys:

* ``name`` usually match with the name of the application
* ``replicaCount`` specifies the number of desired Pods in each cluster
* ``image`` defines the Docker image of the application

    * ``repository`` points to the docker-registry repository where the docker image of your application is stored
    * ``tag`` is the tag of the Docker image, e.g. latest or commit sha

Strategy
--------

Steps
^^^^^

``.spec.template.strategy.steps`` specifies the strategy used to replace old Pods by new ones.
Depending on your needs, you may want to define a more simple or complex strategy,
which translates to a strategy with one or more steps.

Each step contains these values:

* ``name`` of the step, e.g. staging, vanguard, 50/50, fullon
* ``capacity`` as percentage of the final capacity (number of Pods) for a given Release (incumbent or contender). The absolute number is calculated from percentage by rounding up
* ``traffic`` as weight. This define the relative traffic weight for the Pods belonging to a given Release (incumbent or contender). The absolute number is calculated from weights by rounding up

    Both ``capacity`` and ``traffic`` must specify

    * ``incumbent`` defines the current running version (if any)
    * ``contender`` defines the new version that is being rolled out

You can have as many steps in your strategy as you want.

Rolling update
^^^^^^^^^^^^^^

Shipper updates Pods in a rolling update fashion. ``.spec.template.strategy.rollingUpdate`` holds one value
and controls the rolling update process.

``.spec.template.strategy.rollingUpdate.maxSurge`` is an optional field that specifies the maximum number of pods
that can be scheduled above the original number of pods.
Value can be an absolute number (ex: 5) or a percentage of total pods at the start of the update (ex: 10%).
The absolute number is calculated from percentage by rounding up.

By default, a value of 100% is used.

For example, when this is set to 30%, Shipper will move from the current achieved step to the target step in small 30%
increments. This way, the new Release can be scaled up by 30% only. Once old pods have been killed,
new Release can be scaled up further, ensuring that the total number of pods running at any time during the update
is at most 130% of original pods.
When the replica count is set to 10 pods, progressing from step 50/50 to step full on, Shipper will first scale up
3 more contender pods. When contender achieves 8 running pods, 3 incumbent pods will be terminated. Then Shipper will
scale up contender by 2 more pods, and when contender achieves 10 running pods, Shipper will terminate 2 incumbent pods.
Once pods finish terminating, contender has achieved step full on.
