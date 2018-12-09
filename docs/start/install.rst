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
fine. Shipper requires Kubernetes 1.11 or later.

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

Now we should have a working Shipper installation. Let's roll something out!

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

We can check in on the *Release* to see what kind of progress we're making:

.. code-block:: shell

	$ kubectl get rel super-server-83e4eedd-0 -o json | jq .status.achievedStep
	null
	$ # "null" means Shipper has not written the achievedStep key, because it hasn't finished the first step
	$ kubectl get rel -o json | jq .items[0].status.achievedStep
	{
	  "name": "staging",
	  "step": 0
	}

If everything is working, you should see one *Pod* active/ready. Let's advance
the rollout:

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
