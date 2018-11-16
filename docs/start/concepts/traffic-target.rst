.. _concept_traffic_target:

Traffic Target
==============

A *TrafficTarget* is an interface to a method of changing traffic weights: this
may be as simple as editing labels for Service objects or making an API call to
a load balancer, or as complex as orchestrating a full-fledged service mesh
routing stack.

It is manipulated by the Strategy Controller as part of executing a release
strategy.

TrafficTarget Example
---------------------

.. code-block:: yaml

    apiVersion: shipper.booking.com/v1
    kind: TrafficTarget
    metadata:
      name: v0.0.1 # a SHA1 or git tag from the CI/CD pipeline
      labels:
        release: reviewsapi-4
    spec:
      clusters:
      - name: eu-ams-a
        targetTraffic: 0
      - name: eu-ams-b
        targetTraffic: 0
      - name: us-las-a
        targetTraffic: 0
    status:
      clusters:
      - name: eu-ams-a
        achievedTraffic: 0
      - name: eu-ams-b
        achievedTraffic: 0
      - name: us-las-a
        achievedTraffic: 0
        ...

**Disclaimer:** There is a known issue in the current implementation when
using a Kubernetes Service to manage the application's traffic due to the way that
Services work.

The Traffic controller injects for all the Pods spawned by a
Deployment a label stating that this Pod should be included in the
load-balancer pool according to the TrafficTarget object's
specification. Once a deployment is finalized, meaning that the new
release has 100% of traffic, Shipper makes sure that all the Pods
owned by a managed Deployment receive this label. This works reliably
when Shipper is observing the application clusters, except when a
succession of events happen simultaneously:

- Shipper is currently either offline or unable to contact a specific application cluster;
- A cluster node where replicas of Deployments are managed by Shipper misbehaves, making this Deployment's Pods have an Unknown state; and
- Kubernetes schedules new Pods for those Deployments on another node.

If this happens, the newly spawned Pods **will not** be accessible
through Kubernetes load-balancer. The biggest reason for this behavior
is that there is no way to add a label to the
``.spec.template.metadata.labels`` property of a Deployment without
triggering the eviction and re-creation of all the related Pods.
Please note that this is the expected and documented behavior of a
Deployment. Even adding a label to a Deployment's ReplicaSet doesn't
work, since the ReplicaSet ``spec.template`` is hashed and observed by
Kubernetes Deployment controller, and any modifications on it will
result in the creation of another ReplicaSet with the same
``.spec.template`` field existing in the Deployment object.

We **deliberately chose** to not do **anything** regarding this at this time
since we expect that Shipper itself won't have much downtime, and if it loses
connection with application clusters it is likely we have bigger infrastructure
issues. Additionally, we plan to use internal Service Mesh architecture in the
short term, so there's no point to reliably solve this problem *now*.

We've identified a couple of workarounds to mitigate this issue, but all of them
have their pros and cons:

- Do not use Kubernetes internal load-balancer in favor of some Service Mesh implementation.
    - Pros: The problem goes away by not having to label Pods as they're spawned.
    - Cons: Depends on having the right infrastructure in place.

- Implement and install in application clusters a component whose sole responsibility would be to apply traffic related labels into newly spawned Pods.
    - Pros: Application clusters would not require Shipper to maintain the desired traffic state.
    - Cons: Another component that might be subject to failure, now spread on all the application clusters; multiple sources of failure.

- Convert from Deployment to ReplicaSet manifests when installing objects to application clusters.
    - Pros: This would give us the fine grained control we need to make the label problem disappear, since ReplicaSets `.spec.template` can be modified to add the desired traffic label at the end of a deployment, and this just works.
    - Cons: This breaks the user expectations, since they've created a Deployment manifest in their chart, and we then convert it to a ReplicaSet behind the scenes; high WTF levels in here.
