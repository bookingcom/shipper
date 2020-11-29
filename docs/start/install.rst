.. _start:

####################
Shipper in 5 minutes
####################

*************************
Step 0: procure a cluster
*************************

The rest of this document assumes that you have access to a Kubernetes cluster
and admin privileges on it. If you don't have this, check out `docker desktop <https://www.docker.com/products/docker-desktop>`_,
`kind <https://kind.sigs.k8s.io/docs/user/quick-start>`_, `microk8s
<https://microk8s.io/>`_ or `minikube
<https://github.com/kubernetes/minikube>`_. Cloud clusters like GKE are also
fine. Shipper requires Kubernetes 1.17 or later, and you'll need to be an admin
on the cluster you're working with. [#f1]_

Make sure that ``kubectl`` works and can connect to your cluster before
continuing.

------------------------
Setting up kind clusters
------------------------

| How to set-up an application kind cluster and a management kind cluster:
| We would like to setup two clusters, *mgmt* and *app*.
Lets write a ``kind.yaml`` manifest to configure our clusters:

.. code-block:: yaml

    :caption: kind.yaml
    kind: Cluster
    apiVersion: kind.x-k8s.io/v1alpha4
    nodes:
    - role: control-plane
    
Now we'll use this to create the clusters:

.. code-block:: shell

    $ kind create cluster --name app --config kind.yaml --image kindest/node:v1.15.7
    $ kind create cluster --name mgmt --config kind.yaml --image kindest/node:v1.15.7
    
Congratulations, you have created your clusters!

**************************
Step 1: get ``shipperctl``
**************************

``shipperctl`` automates setting up clusters for Shipper. Grab the tarball for
your operating system, extract it, and stick it in your ``PATH`` somewhere.

You can find the binaries on the `GitHub Releases page for
Shipper <https://github.com/bookingcom/shipper/releases>`_.

********************************
Step 2: write a cluster manifest
********************************

``shipperctl`` expects a manifest of clusters to configure. It uses your
``~/.kube/config`` to translate context names into cluster API server URLs.
Find out the name of your context like so:

.. code-block:: shell

    $ kubectl config get-contexts
    CURRENT   NAME              CLUSTER         AUTHINFO            NAMESPACE
              kind-app          kind-app        kind-app
    *         kind-mgmt         kind-mgmt       kind-mgmt

In my setup, the context name of the application cluster is **kind-app**.

This configuration will allow management cluster to communicate with application cluster.
The cluster API server URL stored in the kubeconfig is a local address (127.0.0.1),
we need an actual ip address for our kind-app cluster. This is how you can get it:

.. code-block:: shell

    $ kind get kubeconfig --name app --internal | grep server

Note that **app** is the name we gave to ``kind`` when creating the application cluster.
Copy the URL of the server.

Now let's write a ``clusters.yaml`` manifest to configure Shipper here:

.. code-block:: yaml

    :caption: clusters.yaml

    applicationClusters:
    - name: kind-app
      region: local
      apiMaster: "SERVER_URL"

Paste your server URL as a string.

**************************
Step 3: Setup the Management Cluster
**************************

Before you run ``shipperctl``, make sure that your ``kubectl`` context
is set to the management cluster:

.. code-block:: shell

    $ kubectl config get-contexts
    CURRENT   NAME          CLUSTER                  AUTHINFO            NAMESPACE
              kind-app      kind-app                 kind-app
    *         kind-mgmt     kind-mgmt                kind-mgmt


First we'll setup all the needed resources in the management cluster:

.. code-block:: shell

	$ shipperctl clusters setup management -n shipper-system
	Setting up management cluster:
	Registering or updating custom resource definitions... done
    Creating a namespace called shipper-system... already exists. Skipping
    Creating a namespace called rollout-blocks-global... already exists. Skipping
    Creating a service account called shipper-management-cluster... already exists. Skipping
    Creating a ClusterRole called shipper:management-cluster... already exists. Skipping
    Creating a ClusterRoleBinding called shipper:management-cluster... already exists. Skipping
    Checking if a secret already exists for the validating webhook in the shipper-system namespace... yes. Skipping
    Creating the ValidatingWebhookConfiguration in shipper-system namespace... done
    Creating a Service object for the validating webhook... done
    Finished setting up management cluster

.. _deploy-shipper:
**********************
Step 4: deploy shipper
**********************

Now that we have the namespace, custom resource definitions, role bindings,
service accounts, and so on, let's create the Shipper *Deployment*:

.. code-block:: shell

    $ kubectl --context kind-mgmt create -f https://github.com/bookingcom/shipper/releases/latest/download/shipper.deployment.yaml
    deployment.apps/shipper created

This will create an instance of Shipper in the ``shipper-system`` namespace.

.. join-clusters:
**********************
Step 5: Join the Application cluster to the Management cluster
**********************

Now we'll give ``clusters.yaml`` to ``shipperctl`` to configure the cluster for
Shipper:

.. code-block:: shell

    $ shipperctl clusters join -f clusters.yaml -n shipper-system
    Creating application cluster accounts in cluster kind-app:
    Creating a namespace called shipper-system... already exists. Skipping
    Creating a service account called shipper-application-cluster... already exists. Skipping
    Creating a ClusterRoleBinding called shipper:application-cluster... already exists. Skipping
    Finished creating application cluster accounts in cluster kind-app

    Joining management cluster to application cluster kind-app:
    Creating or updating the cluster object for cluster kind-app on the management cluster... done
    Checking whether a secret for the kind-app cluster exists in the shipper-system namespace... yes. Skipping
    Finished joining management cluster to application cluster kind-app

*********************
Step 6: do a rollout!
*********************

Now you should have a working Shipper installation. :ref:`Let's roll something out! <user_rolling-out>`

.. rubric:: Footnotes

.. [#f1] For example, on GKE you need to `bind yourself to cluster-admin <https://stackoverflow.com/a/52972588>`_ before ``shipperctl`` will work.
