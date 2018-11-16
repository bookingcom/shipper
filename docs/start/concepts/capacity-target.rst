.. _concept_capacity_target:

Capacity Target
===============

A *CapacityTarget* is the interface used by the Strategy Controller to change
the target number of replicas for an application in a set of clusters. It is
acted upon by the Capacity Controller.

The ``status`` resource includes status per-cluster so that the Strategy
Controller can determine when the Capacity Controller is complete and it can
move to the traffic step.

Like the InstallationTarget, since the definition of a desired state heavily
depends on the strategy implemented by the Strategy Controller a global status
for CapacityTarget was omitted.

CapacityTarget Example
----------------------

.. code-block:: yaml

    apiVersion: shipper.booking.com/v1
    kind: CapacityTarget
    metadata:
      name: v0.0.1 # a SHA1 or git tag from the CI/CD pipeline
      annotations:
        "shipper.booking.com/v1/finalReplicaCount": 10
      labels:
        release: reviewsapi-4
    spec:
      clusters:
      - name: eu-ams-a
        percent: 10
      - name: eu-ams-b
        percent: 10
      - name: us-las-a
        percent: 10
    status:
      clusters:
      - name: eu-ams-a
        availableReplicas: 1
        achievedPercent: 10
      - name: eu-ams-b
        availableReplicas: 1
        achievedPercent: 10
      - name: us-las-a
        availableReplicas: 0
        achievedPercent: 0
        sadPods:
        - name: reviews-api-deadbeef
          phase: Terminated
          containers:
          - name: app
            status: CrashLoopBackOff
          condition:
            type: Ready
            status: False
            reason: ContainersNotReady
            message: "unready containers [app]"
