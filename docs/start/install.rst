.. _start:

####################
Shipper in 5 minutes
####################

*************************
Step 0: procure a cluster
*************************

The rest of this document assumes that you have access to a Kubernetes cluster
and admin privileges on it. If you don't have this, check out `microk8s
<https://microk8s.io/>`_ or `minikube
<https://github.com/kubernetes/minikube>`_. Cloud clusters like GKE are also
fine. Shipper requires Kubernetes 1.11 or later, and you'll need to be an admin
on the cluster you're working with. [#f1]_

Make sure that ``kubectl`` works and can connect to your cluster before
continuing.

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
	CURRENT   NAME       CLUSTER            AUTHINFO   NAMESPACE
	*         microk8s   microk8s-cluster   admin

In my setup, the context name is **microk8s**. Let's write a ``clusters.yaml``
manifest to configure Shipper here:

.. code-block:: yaml
    :caption: clusters.yaml

    managementClusters:
    - name: microk8s # name of a context; will also be the Cluster object name
    applicationClusters:
    - name: microk8s
      region: local

**************************
Step 3: apply the manifest
**************************

Now we'll give ``clusters.yaml`` to ``shipperctl`` to configure the cluster for
Shipper:

.. code-block:: shell

	$ shipperctl admin clusters apply -f clusters.yaml
	Setting up management cluster microk8s:
	Registering or updating custom resource definitions... done
	Creating a namespace called shipper-system... done
	Creating a service account called shipper-management-cluster... done
	Creating a ClusterRole called shipper:management-cluster... done
	Creating a ClusterRoleBinding called shipper:management-cluster... done
	Finished setting up cluster microk8s

	Setting up application cluster microk8s:
	Creating a namespace called shipper-system... already exists. Skipping
	Creating a service account called shipper-application-cluster... done
	Creating a ClusterRoleBinding called shipper:application-cluster... done
	Finished setting up cluster microk8s

	Joining management cluster microk8s to application cluster microk8s:
	Creating or updating the cluster object for cluster microk8s on the management cluster... done
	Checking whether a secret for the microk8s cluster exists in the shipper-system namespace... no. Fetching secret for service account shipper-application-cluster from the microk8s cluster... done
	Copying the secret to the management cluster... done
	Finished joining cluster microk8s and microk8s together

	Cluster configuration applied successfully!

**********************
Step 4: deploy shipper
**********************

Now that we have the namespace, custom resource definitions, role bindings,
service accounts, and so on, let's create the Shipper *Deployment*:

.. code-block:: shell

    $ kubectl create -f https://github.com/bookingcom/shipper/releases/download/v0.1.0/shipper-deploy.yaml
    deployment.apps/shipper created

This will create an instance of Shipper in the ``shipper-system`` namespace.

*********************
Step 5: do a rollout!
*********************

Now we should have a working Shipper installation. :ref:`Let's roll something out! <user_rolling-out>`

.. rubric:: Footnotes

.. [#f1] For example, on GKE you need to `bind yourself to cluster-admin <https://stackoverflow.com/a/52972588>`_ before ``shipperctl`` will work.
