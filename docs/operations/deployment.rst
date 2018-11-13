.. _operations_deployment:

Deployments with Shipper
========================

To deploy your service using _Shipper_ you need to create an `Application <_getting_started_Application>`_ object.

Required Fields
~~~~~~~~~~~~~~~

In your Application's object you'll find the following fields, just like any Kubernetes object:

``apiVersion``
    Which version of the _Shipper_ API you're using (default: `shipper.booking.com/v1`)
``kind``
    What kind of object you want to create
``metadata``
    Data that helps uniquely identify the object, including a `name` String.
``spec``
    Specification of the desired behavior of the Shipper Application.

Metadata
~~~~~~~~

Standard object's metadata. More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata.

``name``
    The name of your application.
``labels``
    Map of string keys and values that can be used to organize and categorize (scope and select) objects. All chart objects created by a Release of this application will inherit labels from the Application. More info: http://kubernetes.io/docs/user-guide/labels.

Specification
~~~~~~~~~~~~~

``revisionHistoryLimit``
    The number of old Release objects to keep around to retain to allow rollback. This is a pointer to distinguish between explicit zero and not specified. In development we don't need to keep any history, so value would be one, while in a production environment you'd likely want some rollback targets.
``template``
    Is a mold for stamping out new Release objects, one per rollout. Every time you cange the template for your Application in the cluster, *Shipper* will create a Release and perform a rollout.

Cluster Requirements
********************

In Application's  ``.spec.template`` field we can define the clusters where our application should be deployed. This information is provided in the `clusterRequirements` field.

``clusterRequirements.regions``
    A list of objects representing regions with Kubernetes clusters available.

``clusterRequirements.regions[].name``
    Name of the region to pick a cluster from.

``clusterRequirements.regions[].replicas``
    Determines how many clusters your application should be scheduled on in this region.

``clusterRequirements.capabilities``
    Clusters have capabilities, which are a list of arbitrary strings representing different features available to workloads in that cluster. For example, perhaps a subset of clusters are provisioned with SSD backed local storage. As a workload, you can require that any cluster you're assigned to have a particular set of capabilities.

Chart
*****

Inside the ``template`` section we can define the base chart that our application is going to use. This information is provided in the ``chart`` section. A chart is a package of templates for kubernetes configuration files (please refer to `this document <https://docs.helm.sh/developing_charts/>`_ on how to create your own chart). As your application grows and becomes more specialized, you can change this to be a chart specific to your application. You shouldn't need to change this too often: the most dynamic parts of the config are contained in 'values'

``chart.name``
    Chart's name

``chart.version``
    Chart's version

``chart.repoUrl``
    Chart repository to retrieve the chart.

.. note:: Shipper will cache this chart version internally after fetching to protect against repository outages. This means that if you need to change your chart, you need to tag it with a different version.

Values
******

Inside the ``template`` section ``values`` apply to the defined ``chart``.

``values``
    Are what the chart templates get as parameters when they render. Some of these will likely change every rollout as you update the tag of the docker image you want to deploy.

  These are specific to the Chart you are deploying. Below you can find some examples:

    ``name``
        Usually matches ``metadata.name``, used for...

    ``replicaCount``
        Determines how many pods should be scheduled in each cluster.

    ``image.repository``
        Points to the docker-registry repository where the docker image of your application is stored.

    ``image.tag``
        Informs Kubernetes which Docker image's tag to use when deploying your application.

Strategy
********

Depending on the environment we may want to define a more simple or complex strategy, which translates to a strategy with one or more steps. This is configured using the the ``strategy`` field, which defines the rollout strategy of your application.

``strategy.steps[]``
    In a development environment we don't need too much of a rollout strategy, so a single step will be enough, while in production we may want to have multiple steps as `staging`, `canary` and `full on` (more here (note: point to example of each strategy for copy-pasta)).

``strategy.steps[].name``
    Step name, some typical examples would be `staging`, `canary` and `full on`.

``strategy.steps[].capacity.contender``
    These values are percentages of the capacity for the contender Release for a strategy step, which will be translated to a concrete number of Pods when deploying your application.

``strategy.steps[].capacity.incumbent``
    These values are percentages of the capacity for the incumbent Release (if any) for a strategy step, which will be translated to a concrete number of Pods when deploying your application.

    .. important:: Percentages are always rounded up to the nearest whole Pod, ensuring that capacity should be provided for calculations that end up resulting in fractions of a single Pod.

    .. note:: *Contender* is a Release Shipper is busy deploying (for example, after you've modified the Application's ``template`` field. *Incumbent*, on the other hand, is a Release Shipper has been deployed before and is currently being phased out in favor of the *contender* Release.

``strategy.steps[].traffic.contender``
    How much traffic should this Release get? These values are weights, not percentages: they could just as well be '9' and '1' or '900' and '100'. They specify the relative traffic weight for the pods belonging to a given Release, __incumbent__ (old) or __contender__ (new).

You can have as many steps in your Strategy as you want. The default offering
has 3 steps, staging, canary, and full on.
