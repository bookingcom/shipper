.. _concept_application:

Application
===========

An *Application* represents a single deployable application in Shipper. It
resides in a single namespace. If this object does not exist for a given user
application, the deployment system does not manage or deploy that application.

It is the primary interface for kicking off new deployments. This is
accomplished by editing (``kubectl replace|apply|edit``) the ``template``
section of the ``spec``: this ``template`` is for new *Release* objects. This is
comparable to a Kubernetes ``Deployment`` object and its ``template`` for pods.

The ``template`` can express all of the fields present in ``.metadata.environment`` for a *Release*. Some are optional: for example, clusters may be left off to allow the Schedule Controller to automatically select clusters.

Scope of control
----------------

The deployment system only manages target cluster resources which
are associated with an Application in the corresponding management cluster
namespace. The deployment system does not manage entire namespaces.

For example: the deployment system deploys chart ``reviewsapi-0.0.1`` to target cluster
namespace ``usercontent`` based on an application ``reviewsapi`` in management
cluster namespace ``usercontent``. The ``reviewsapi-0.0.1`` chart contains only
a Deployment and a ConfigMap. If there is a Secret ``foobar`` in ``usercontent``,
the deployment system will not interact with that Secret: it is outside of
the deployment system's control because it is not associated with an
Application object.

Application Example
-------------------

.. code-block:: yaml

    apiVersion: shipper.booking.com/v1
    kind: Application
    metadata:
      name: reviewsapi # canonical name of the application.
    spec:
      template:
        clusterRequirements:
          regions:
          - name: eu-ams
          - name: us-west-1
          capabilities:
          - gpu
          - pci
        chart:
          name: reviewsapi
          version: 0.0.1
        strategy:
          name: vanguard
        values:
          perl:
            image:
              name: myorg/my-cool-app
              version: SHA256
