.. _concepts_release:

Release
=======

A *Release* encodes all of the information required to rollout (or rollback) a
particular version of an application. It represents the transition from the
incumbent version to the candidate version.

It consists primarily of an immutable ``.metadata.environment`` from the
Application ``template``. This environment is annotated by the Schedule
controller to transform ``clusterRequirements`` into concrete clusters; it may
also contain mandatory sidecars or anything else required to install or upgrade
the application in a cluster.

Additionally, its ``spec`` is the interface used by the outside world to indicate
that it is safe to proceed with a release strategy.

Its ``status`` sub-object is used by various controllers to encode the position
of this operation in the overall release state machine.

The *Phase* field in the Status object indicates the deployment/release phase
the system is waiting for completion, and it is up to the aforementioned various
controllers to determine whether there is work to be done, and also is their
responsibilities to modify the field to activate the next phase.

The ``release`` label is used to further identify all the Kubernetes objects
that were created by this particular release.

Release Example
---------------

.. code-block:: yaml

    apiVersion: shipper.booking.com/v1
    kind: Release
    metadata:
      name: v0.0.1 # a SHA1 or git tag from the CI/CD pipeline
      labels:
        release: reviewsapi-4
    environment:
      replicas: 20
      clusters:
        - eu-ams-a
        - eu-ams-b
        - us-las-a
      chart:
        name: reviewsapi
        version: 0.0.1
        repoUrl: localhost
      clusterRequirements: ...
      strategy:
        name: vanguard
      values: ...
      sidecars:
        - name: envoy
          version: 2.1
        - name: telegraf
          version: 0.99
    spec:
      targetStep: 1
    status:
      phase: WaitingForStrategy
      achievedStep: 0
      conditions:
      - lastTransitionTime: 2018-01-11T13:17:22Z
        type: WaitingForStrategy
      - lastTransitionTime: 2018-01-11T13:17:19Z
        type: WaitingForScheduling
