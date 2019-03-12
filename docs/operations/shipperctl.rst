.. _operations_shipperctl:

======================
 Using ``shipperctl``
======================

The ``shipperctl`` command is created to make using Shipper easier. The commands under ``shipperctl admin`` are meant to aid cluster administrators or users who want to administrate Shipper locally. Commands that are not a subset of ``shipperctl admin`` are meant to make life easier for users using a cluster with Shipper already running in it.

Setting Up Clusters Using ``shipperctl admin clusters apply``
-------------------------------------------------------------

To set up clusters to work with Shipper, you should create *ClusterRoleBindings*, *ClusterRoles*, *Roles*, *RoleBindings*, *Clusters*, and so forth.

Meet ``shipperctl admin clusters apply``, which is made to make this easier.

There are two use cases for this command.

First, you can use it to set up a local environment to run Shipper in, or to set up a fleet of clusters for the first time.

Second, you can integrate it into your continuous integration pipeline. Since this command is idempotent, you can use it to apply the configuration of your clusters. Here is how you would do that:

- Create the configuration file, defining your clusters. The configuration file is explained below
- Run ``shipperctl admin clusters apply -f clusters.yaml`` as part of your CI/CD pipeline
- Change the file later on, commit it to your repository, and ``shipperctl`` will apply your changes for you

Options
^^^^^^^

.. option:: -f <path string>

  The path to the cluster configuration file. The format is explained below.

.. option:: --kube-config <path string>

  The path to your ``kubectl`` configuration, where the contexts that ``shipperctl`` should use resides.

.. option:: -n, --shipper-system-namespace <string>

  The namespace Shipper is running in. This is the namespace where you have a *Deployment* running the Shipper image.

Clusters Configuration File Format
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

The clusters configuration file is a *YAML* file. At the top level, you should specify two keys, ``managementClusters`` and ``applicationClusters``. The clusters you specify under each key are your **management** and **application** clusters, respectively. Check out :ref:`Cluster Architecture <operations_cluster-architecture>` to learn more about what this means.

For each item in the list of **management** or **application** clusters, you can specify these fields:

- name (mandatory): This is the name of the cluster. When specified for an **application** cluster, a :ref:`Cluster <api-reference_cluster>` object will be created on the **management** cluster, and will point to the **application**.
- context (optional, defaults to the value of ``name``): this is the name of the *context* from your *kubectl* configuration that points to this cluster. ``shipperctl`` will use this context to run commands to set up the cluster, and also to populate the URL to the ``api-master``.
- Fields from the :ref:`Cluster <api-reference_cluster>` object (optional): you can specify any field from the *Cluster* object, and ``shipperctl`` will patch the Cluster object for you the next time you run it. The only field that is mandatory is ``region``, which you have to specify to create any *Cluster* object.

Examples
^^^^^^^^

Minimal Configuration
~~~~~~~~~~~~~~~~~~~~~

Here is a minimal configuration to set up a local *minikube* instance:

.. code-block:: yaml

  managementClusters:
  - name: minikube
  applicationClusters:
  - name: minikube
    region: eu-west

This way, setting up an environment to run Shipper in *Docker For Desktop*, for example, is as easy as creating a list of ``managementClusters`` and a list of ``applicationClusters``, and specifying ``docker-for-desktop`` as the name.

Specifying Cluster Fields
~~~~~~~~~~~~~~~~~~~~~~~~~

Here is something more interesting: having 2 application clusters, and marking one of them as unschedulable:

.. code-block:: yaml

  managementCluster:
  - name: eu-m
  applicationClusters:
  - name: eu-1
    region: eu-west
  - name: eu-2
    region: eu-west
    scheduler:
      unschedulable: true

Using Google Kubernetes Engine (GKE) Context Names
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

If you're running on GKE, your cluster context names are likely to have underscores in them, like this: ``gke_ACCOUNT_ZONE_CLUSTERNAME``. ``shipperctl``'s usage of the context name as the name of the Cluster object will break, because Kubernetes objects are not allowed to have underscores in their names. To solve this, specify ``context`` explicitly in ``clusters.yaml``, like so:

.. code-block:: yaml

  managementCluster:
  - name: eu-m # make sure this is a Kubernetes-friendly name
    context: gke_ACCOUNT_ZONE_CLUSTERNAME_MANAGEMENT # add this
  applicationClusters:
  - name: eu-1
    region: eu-west
    context: gke_ACCOUNT_ZONE_CLUSTERNAME_APP_1 # same here
  - name: eu-2
    region: eu-west
    context: gke_ACCOUNT_ZONE_CLUSTERNAME_APP_2 # and here
    scheduler:
      unschedulable: true
