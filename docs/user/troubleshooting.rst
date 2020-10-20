.. _user_troubleshooting:

Troubleshooting Shipper
=======================

Prerequisites
-------------

To troubleshoot deployments effectively you need to be familiar with `core Kubernetes <https://kubernetes.io/docs/concepts/>`_ and Shipper concepts (*very briefly* explained below) and be comfortable running `kubectl` commands.

Fundamentals
------------

Shipper objects form a hierarchy:

.. code-block:: text

    Application
        |
    Release
        |
    InstallationTarget
    CapacityTarget
    TrafficTarget

You already know Applications and Releases, but there's more. Below Releases you
have what we call "target objects". Each represents an important chunk of work
we do when rolling out:

.. list-table::
    :widths: 1 1 98
    :header-rows: 1

    * - Kind
      - Shorthand
      - Description
    * - InstallationTarget
      - it
      - Install charts in :ref:`application clusters <operations_cluster-architecture_application-cluster>`
    * - CapacityTarget
      - ct
      - Scale deployments up and down to reach desired number of pods
    * - TrafficTarget
      - tt
      - Orchestrate traffic by moving pods in and out of the LB

The list is ordered (e.g. we can't manipulate traffic before there are pods).

The universal troubleshooting algorithm
---------------------------------------

Shipper is a fairly complex system that runs on top of an even more complex one.
Things can fail in many different ways. It's not really feasible for us to list
all the possible problems and solutions for them. Instead, we'll give you a
rough algorithm that should help you deal with commonly encountered problems.

To summarise, the algorithm is roughly:

1. Find what stage you're at by looking at Release conditions and state
2. Inspect the corresponding target object's conditions
3. Act accordingly

In the next sections we'll explain in more detail how to do that.

Finding where you are
~~~~~~~~~~~~~~~~~~~~~

Before we attempt to fix anything we need to make sure we know where we are in
the rollout process. The starting point is almost always looking at your
Release's status:

.. code-block:: shell

    $ kubectl describe rel nginx-vj7sn-7cb440f1-0
    ...
    Status:
      Achieved Step:
        Name:  staging
        Step:  0
      Conditions:
        Last Transition Time:  2018-07-27T07:21:14Z
        Status:                True
        Type:                  Scheduled
      Strategy:
        Conditions:
          Last Transition Time:  2018-07-27T07:23:29Z
          Message:               clusters pending capacity adjustments: [minikube]
          Reason:                ClustersNotReady
          Status:                False
          Type:                  ContenderAchievedCapacity
          Last Transition Time:  2018-07-27T07:23:29Z
          Status:                True
          Type:                  ContenderAchievedInstallation
        State:
          Waiting For Capacity:      True
          Waiting For Command:       False
          Waiting For Installation:  False
          Waiting For Traffic:       False
    ...

We already looked at `status.strategy.state.waitingForCommand` but there are more fields there: one for every type of target objects. If your rollout isn't finished and not waiting for input, these fields tell you which stage you're at.

.. list-table::
    :widths: 25 75
    :header-rows: 1

    * - Field
      - Meaning
    * - ``waitingForInstallation``
      - Waiting for the chart to be installed in application clusters
    * - ``waitingForCapacity``
      - Waiting for the contender to scale up and/or the incumbent to scale down
    * - ``waitingForTraffic``
      - Waiting for the contender traffic to increase and/or the incumbent to
        decrease

Release conditions and strategy conditions
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

.. list-table::
    :widths: 25 75
    :header-rows: 1

    * - Category
      - Description
    * - Object conditions
      - Conditions that apply to the object itself. **All** objects have this.
    * - Strategy conditions
      - Conditions that apply to the strategy of the Release that's being rolled out. Only **Releases** have this.

In the example above, under ``.status.strategy`` we can find a condition called ``ContenderAchievedCapacity``, saying there're still clusters pending capacity adjustments.

Target objects
~~~~~~~~~~~~~~

The next step would be to look at the corresponding target object. Since we're waiting for capacity, we'll be looking at CapacityTarget. The object will have the same name as the release but different kind:

.. code-block:: shell

    $ kubectl describe ct nginx-vj7sn-7cb440f1-0
    ...
    Status:
      Clusters:
        Achieved Percent:    0
        Available Replicas:  0
        Conditions:
          Last Transition Time:  2018-07-27T07:23:29Z
          Status:                True
          Type:                  Operational
          Last Transition Time:  2018-07-27T07:23:29Z
          Message:               there are 1 sad pods
          Reason:                PodsNotReady
          Status:                False
          Type:                  Ready
        Name:                    minikube
        Sad Pods:
          Condition:
            Last Probe Time:       <nil>
            Last Transition Time:  2018-07-27T07:23:14Z
            Status:                True
            Type:                  PodScheduled
          Containers:
            Image:     nginx:boom
            Image ID:
            Last State:
            Name:           nginx
            Ready:          false
            Restart Count:  0
            State:
              Waiting:
                Message:    Back-off pulling image "nginx:boom"
                Reason:     ImagePullBackOff
          Init Containers:  <nil>
          Name:             nginx-vj7sn-7cb440f1-0-nginx-9b5c4d7c9-2gjwl
    ...

.. important::
    For installation the command would be ``kubectl describe it <release name>``,
    for traffic ``kubectl describe tt <release name>``.

If we inspect ``.status.conditions`` of the InstallationTarget we'll notice a condition called ``Ready`` which has status ``False`` and reason ``PodsNotReady``. Further inspection will reveal that we have a pod called ``nginx-vj7sn-7cb440f1-0-nginx-9b5c4d7c9-2gjwl`` and that Kubernetes can't pull the Docker image for one if its containers:

.. code-block:: text

    Message:    Back-off pulling image "nginx:boom"
    Reason:     ImagePullBackOff

The "boom" Docker tag clearly looks wrong. To fix this you can simply edit the application object and set the correct tag in `.spec.template.values`.

Other sources of useful information
-----------------------------------

Shipper emits Kubernetes events with useful information. You can look at that, if you prefer:

.. code-block:: shell

    $ kubectl get events
    ...
    1m          1h           238       nginx-vj7sn-7cb440f1-0.154528eb631aac75                         CapacityTarget                                Normal    CapacityTargetChanged       capacity-controller       Set "default/nginx-vj7sn-7cb440f1-0" status to {[{minikube 0 0 [{nginx-vj7sn-7cb440f1-0-nginx-9b5c4d7c9-2gjwl [{nginx {&ContainerStateWaiting{Reason:ImagePullBackOff,Message:Back-off pulling image "nginx:boom",} nil nil} {nil nil nil} false 0 nginx:boom  }] [] {PodScheduled True 0001-01-01 00:00:00 +0000 UTC 2018-07-27 09:23:14 +0200 CEST  }}] [{Operational True 2018-07-27 09:23:29 +0200 CEST  } {Ready False 2018-07-27 09:23:29 +0200 CEST PodsNotReady there are 1 sad pods}]}]}

Typical failure scenarios
-------------------------

While we can't list all the possible failures we can list the ones that we
think happen more often than others:

.. list-table::
    :widths: 25 75
    :header-rows: 1

    * - Failure
      - Description
    * - | Can't pull Docker image
      - Strategy condition ``ContenderAchievedCapacity`` is false,
        InstallationTarget's ``Ready`` condition is false and the message is
        something like "Back-off pulling image "nginx:boom""
    * - Previous release is unhealthy
      - Release condition ``IncumbentAchievedCapacity`` is false and the
        message is something like "incumbent capacity is unhealthy in clusters:
        [minikube]". In this case, you can try describing the CapacityTarget
        from the previous release to find out what's wrong. If you're doing a
        rollout to fix that previous release, though, you can opt for
        proceeding to the next step in your strategy, as Shipper does not
        require a step to be completed before moving on to the next.
    * - Can't fetch Helm chart
      - Release condition ``Scheduled`` is false and the message is something
        like "download https://charts.example.com/charts/nginx-0.1.42.tgz: 404"

Make sure you're on the right cluster!
--------------------------------------

There are cases where the user is checking on the wrong cluster and can't see the pods etc. To make sure you're on the right one:

.. code-block:: shell

    $ kubectl get release
    NAME                       CREATED AT
    myrelease-cf68dfe8-0       23m

    $ kubectl describe release <your app release> | grep release.clusters
    Annotations:  shipper.booking.com/release.clusters=kube-us-east-1-a
