.. _concept_installation_target:

Installation Target
===================

An *InstallationTarget* describes the concrete set of clusters where the release
should be installed. It is created by the Schedule Controller after the
concrete clusters are picked using ``clusterRequirements``.

The Installation Controller acts on InstallationTarget objects by getting the
chart, values, and sidecars from the associated Release object,
rendering the chart per-cluster, and inserting those objects into each target
cluster. Where applicable, these objects are always created with 0 replicas.

It updates the ``status`` resource to indicate progress for each target cluster.

Since the definition of a desired state heavily depends on the strategy
implemented by the Strategy Controller, as in some strategies being more lenient
than other with respect to the installation process, a global status for
InstallationTarget was omitted.

InstallationTarget Example
--------------------------

.. code-block:: yaml

    apiVersion: shipper.booking.com/v1
    kind: InstallationTarget
    metadata:
      name: v0.0.1 # a SHA1 or git tag from the CI/CD pipeline
      labels:
        release: reviewsapi-4
    spec:
      clusters:
      - eu-ams-a
      - eu-ams-b
      - us-las-a
    status:
      clusters:
      - name: eu-ams-a
        status: Installed
        conditions:
        - lastProbeTime: null
          lastTransitionTime: 2018-01-11T13:17:19Z
          status: "True"
          type: Installed
      - name: us-las-a
        status: WaitingForInstall
        conditions: []
        ...
