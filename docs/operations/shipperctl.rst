.. _operations_shipperctl:

======================
Using ``shipperctl``
======================

The ``shipperctl`` command is created to make using Shipper
easier.

Setting Up Clusters Using ``shipperctl clusters`` Commands
-------------------------------------------------------------

To set up clusters to work with Shipper, you should create
*ClusterRoleBindings*, *ClusterRoles*, *Roles*, *RoleBindings*,
*Clusters*, and so forth.

Meet ``shipperctl clusters``, which is made to make this easier.

There are two use cases for this set of commands.

First, you can use it to set up a local environment to run Shipper in,
or to set up a fleet of clusters for the first time.

Second, you can integrate it into your continuous integration pipeline.
Since these commands are idempotent, you can use it to apply the configuration
of your clusters.

Note that these commands don't apply a Shipper deployment. You should :ref:`deploy Shipper <start>` once
you've run these commands.

The commands under ``shipperctl clusters`` should be run in this order
if you're setting up a cluster for a very first time. Once you've
followed this procedure, you can use the ones that apply to your
situation.

.. important::

Note that you need to change your context to point to the
   management cluster before running the following commands.

#. `shipperctl clusters setup management`_: creates the
   *CustomResourceDefinitions*, *ServiceAccount*, *ClusterRoleBinding*
   and other objects Shipper needs to function correctly.
#. `shipperctl clusters join`_: creates the *ServiceAccount* that
   Shipper is going to use on the **application** cluster, and copies
   its token back to the **management** cluster. This is so that
   *Shipper*, which runs on the **management** cluster, can
   modify Kubernetes objects
   on the **application** cluster. Once the token is created,
   this command also creates a *Cluster* object on the *management*
   cluster, which tells Shipper how to communicate with the
   **application** cluster.

All of these commands share a certain set of options. However, they
each have their own set of options as well.

Below are the options that are shared between all the commands:

.. option:: --kube-config <path string>

  The path to your ``kubectl`` configuration, where the contexts that ``shipperctl`` should use reside.

.. option:: -n, --shipper-system-namespace <string>

  The namespace Shipper is running in. This is the namespace where you have a *Deployment* running the Shipper image.

.. option:: --management-cluster-context <string>

  By default, ``shipperctl`` uses the context that was already set in your ``kubeconfig``
(i.e. using ``kubectl config use-context``). However, if that's not what you want,
you can use this option to tell ``shipperctl`` to use another context.

``shipperctl clusters setup management``
++++++++++++++++++++++++++++++++++++++++

As mentioned above, this command is used to set up the **management** cluster for use with Shipper.

.. option:: --management-cluster-service-account <string>

  the name of the service account Shipper will use for the management cluster (default "shipper-mgmt-cluster")

.. option:: -g, --rollout-blocks-global-namespace <string>

  the namespace where global RolloutBlocks should be created (default "rollout-blocks-global")

  This is the namespace that the users or administrators of the
  **management** cluster will create a *RolloutBlock* object, so that
  all Shipper rollouts for *Applications* on that cluster would be
  disabled.

``shipperctl clusters join``
++++++++++++++++++++++++++++

As mentioned above, this command is used to join the **management** and
**application** clusters together using a ``clusters.yaml`` file. To
know more about the format of that file, look at the `Clusters
Configuration File Format`_ section.

.. option:: --application-cluster-service-account <string>

  the name of the service account Shipper will use in the application cluster (default "shipper-app-cluster")

.. option:: -f, --file <string>

  the path to a YAML file containing application cluster configuration (default "clusters.yaml")

Clusters Configuration File Format
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

The clusters configuration file is a *YAML* file. At the top level,
you should specify two keys, ``managementClusters`` and ``applicationClusters``.
The clusters you specify under each key are your **management** and **application**
clusters, respectively. Check out :ref:`Cluster Architecture <operations_cluster-architecture>`
to learn more about what this means.

For each item in the list of **management** or **application** clusters, you can specify these fields:

- name (mandatory): This is the name of the cluster. When specified for an **application** cluster,
a :ref:`Cluster <api-reference_cluster>` object will be created on the **management** cluster,
and will point to the **application**.
- context (optional, defaults to the value of ``name``): this is the name of the *context* from your
*kubectl* configuration that points to this cluster. ``shipperctl`` will use this context to run
commands to set up the cluster, and also to populate the URL of the API master.
- Fields from the :ref:`Cluster <api-reference_cluster>` object (optional):
you can specify any field from the *Cluster* object, and ``shipperctl`` will patch the
Cluster object for you the next time you run it. The only field that is mandatory is ``region``,
which you have to specify to create any *Cluster* object.

Examples
````````

Minimal Configuration
~~~~~~~~~~~~~~~~~~~~~

Here is a minimal configuration to set up a local *kind* instance, assuming that you have
created a cluster called ``mgmt`` and a cluster called ``app``:

.. code-block:: yaml

  managementClusters:
  - name: kind-mgmt # kind contexts are prefixed with `kind-`
  applicationClusters:
  - name: kind-app
    region: local

Specifying Cluster Fields
~~~~~~~~~~~~~~~~~~~~~~~~~

Here is something more interesting: having 2 application clusters, and
marking one of them as unschedulable:

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

If you're running on GKE, your cluster context names are likely to have underscores in them,
like this: ``gke_ACCOUNT_ZONE_CLUSTERNAME``. ``shipperctl``'s usage of the context name as the
name of the Cluster object will break, because Kubernetes objects are not allowed to have
underscores in their names. To solve this, specify ``context`` explicitly in ``clusters.yaml``, like so:

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


Creating backups and restoring Using ``shipperctl backup`` Commands
----------------------------------------------------------------------

.. _create_backup:

``shipperctl backup prepare``
+++++++++++++++++++++++++++++++

1. The backup must be created by a `shipperctl` command. This guarantees you can restore this backup.
Acquire a backup file by running

.. code-block:: bash

    $ kubectl config use-context mgmt-dev-cluster ##be sure to switch to correct context of the management cluster before backing up
    Switched to context "mgmt-dev-cluster"
    $ shipperctl backup prepare -v -f bkup-dev-29-10.yaml
    NAMESPACE  RELEASE NAME              OWNING APPLICATION
    default    super-server-dc5bfc5a-0   super-server
    default2   super-server2-dc5bfc5a-0  super-server2
    default3   super-server3-dc5bfc5a-0  super-server3
    Backup objects stored in "bkup-dev-29-10.yaml"


.. epigraph::

    The command's default format is yaml. This will create a file named "bkup-dev-29-10.yaml" and store the backup there in a yaml format.

2. Save the backup file in a storage system of your liking (for example, AWS S3)

3. That's it! Repeat steps 1+2 for all management clusters.

``shipperctl backup restore``
+++++++++++++++++++++++++++++++++

1. Download your latest backup from your selected storing system

2. Make sure that Shipper is down (`spec.replicas: 0`) before applying objects.

3. Use `shipperctl` to restore your backup:

.. code-block:: bash

    $ kubectl config use-context mgmt-dev-cluster ##be sure to switch to correct management context before restoring backing up
    Switched to context "mgmt-dev-cluster"
    $ shipperctl backup restore -v -f bkup-dev-29-10-from-s3.yaml
    Would you like to see an overview of your backup? [y/n]: y
    NAMESPACE  RELEASE NAME              OWNING APPLICATION
    default    super-server-dc5bfc5a-0   super-server
    default2   super-server2-dc5bfc5a-0  super-server2
    default3   super-server3-dc5bfc5a-0  super-server3
    Would you like to review backup? [y/n]: y
    - application:
        apiVersion: shipper.booking.com/v1alpha1
        kind: Application
      ...
      backup_releases:
      - capacity_target:
          apiVersion: shipper.booking.com/v1alpha1
          kind: CapacityTarget
        ...
        installation_target:
          apiVersion: shipper.booking.com/v1alpha1
          kind: InstallationTarget
        ...
        release:
          apiVersion: shipper.booking.com/v1alpha1
          kind: Release
        ...
        traffic_target:
          apiVersion: shipper.booking.com/v1alpha1
          kind: TrafficTarget
        ...
    ...
    Would you like to restore backup? [y/n]: y
    application "default/super-server" created
    release "default/super-server-dc5bfc5a-0" owner reference updates with uid "a6c587cb-624e-44ec-b267-b48630b0ed1c"
    release "default/super-server-dc5bfc5a-0" created
    installation target "default/super-server-dc5bfc5a-0" owner reference updates with uid "9ccfd876-7f4f-4b1c-9c10-653d295e21d2"
    installation target "default/super-server-dc5bfc5a-0" created
    traffic target "default/super-server-dc5bfc5a-0" owner reference updates with uid "9ccfd876-7f4f-4b1c-9c10-653d295e21d2"
    traffic target "default/super-server-dc5bfc5a-0" created
    capacity target "default/super-server-dc5bfc5a-0" owner reference updates with uid "9ccfd876-7f4f-4b1c-9c10-653d295e21d2"
    capacity target "default/super-server-dc5bfc5a-0" created
    ...

.. epigraph::

     - The command's default format is yaml. This will apply the backup from file "bkup-dev-29-10-from-s3.yaml" while maintaining owner references between an application and its releases and between release and its target objects.
     - The backup file must be created using :ref:`shipperctl backup prepare <create_backup>` command.
