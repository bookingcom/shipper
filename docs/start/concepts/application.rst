.. _concept_application:

Application
===========

An *Application* represents a single application [#]_ Shipper can manage on
a user's behalf.

The Application object is the primary interface to interact with Shipper. This
is accomplished by editing an application's ``.spec.template`` field. The
*template* field is input that Shipper will use to create a
*Release* object, which is comparable to a Kubernetes
*Deployment* object and its ``.spec.template`` field, which is used as template
for *Pod* objects.

The ``.spec.template`` field will be copied to a new *Release*
object under the ``.spec.environment`` field during deployment.

Scope of control
----------------

*Application* objects own *Release* objects through Kubernetes
`owner and dependents <https://kubernetes.io/docs/concepts/workloads/controllers/garbage-collection/#owners-and-dependents>`_
mechanism in the management cluster. No other objects are directly associated
with *Application* objects. Through Kubernetes garbage collection mechanism,
*Release* and its dependent objects are disposed when *Application* objects
are removed from the management cluster.

After a user successfully creates an *Application* object on the management
cluster, Shipper starts to work on it by creating the application's first
*Release* object. As stated earlier, the contents of the application's
``.spec.template`` are used when creating this new *Release* object. The
action of creating a new release triggers other Shipper processes (which we'll
be covering in `here <concepts_release>`_).

Every modification in an application's ``.spec.template`` field will,
unconditionally, create a new *Release* object.

In the case the latest release object hasn't been fully deployed, a
new *Release* object will be created and the incomplete one **removed**.
This was designed to allow users to fix a broken release (for example,
fixing the Docker image's tag) without having to remove the existing
release.

In the case the latest release object **has been** fully deployed, a new
*Release* object will be created and a **transition** will start, from the
current deployed *Release* object (the **incumbent** release) to the newly
created *Release* object (the **contender** release).

.. note:: Please check the `Release <concepts_release>`_ documentation to
          learn how to **abort** an ongoing release deployment, or how to **revert**
          to a previous, already deployed *Release* object.

Writing an Application Spec
---------------------------

Revision History Limit
~~~~~~~~~~~~~~~~~~~~~~

``.spec.revisionHistoryLimit`` is an optional field that represents the number of associated :ref:`Release <concepts_release>` objects in ``.status.history``.

Release Template
~~~~~~~~~~~~~~~~

The ``.spec.template`` is the only required field of the ``.spec``.

The ``.spec.template`` is a release template. It has the same schema as :ref:`Release environment <concepts_release_environment>`.

Application Example
-------------------

.. code-block:: yaml

    apiVersion: shipper.booking.com/v1
    kind: Application
    metadata:
      name: reviews-api
    spec:
      revisionHistoryLimit: 10
      template:
        chart:
          name: reviews-api
          repoUrl: https://charts.example.com
          version: 0.0.1
        clusterRequirements:
          capabilities:
          - gpu
          - pci
          regions:
          - name: eu-ams
          - name: us-west-1
        strategy: {}
        values:
          perl:
            image:
              name: myorg/my-cool-app
              version: SHA256

.. rubric:: Footnotes
.. [#] https://en.wikipedia.org/wiki/Application_software
