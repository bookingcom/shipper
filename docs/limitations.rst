.. _limitations:

############################
Limitations and known issues
############################

Shipper is just software, and all software has limits. Here are the highlights
for Shipper currently. Some of these are not principal problems, just shortcuts
that we took while building Shipper.

******************
Chart restrictions
******************

Shipper expects a few properties to be true about the Chart it is rolling out.
We hope to loosen or remove most of these restrictions over time.

Only *Deployments*
------------------

The Chart must have exactly one *Deployment* object. The name of the
*Deployment* should be templated with ``{{.Release.Name}}``. The *Deployment*
object should have ``apiVersion: apps/v1``. 

Shipper cannot yet perform roll outs for *StatefulSets*,
*HorizontalPodAutoscalers*, or bare *ReplicaSets*. These objects can be
present in the Chart, but Shipper only knows how to manipulate *Deployment*
objects to scale capacity over the course of a rollout.

*Services*
----------

The Chart must contain either:

    - exactly one *Service*, or
    - exactly one *Service* labeled with the label ``shipper-lb: production``.

The name of the *Service* should be fixed: either a literal in the Chart
template, or a value which does not change from release to release.

The *Service* should have a ``selector`` which matches the application, not
a single release. A *Service* with ``release: {{ .Release.Name }}`` as part
of the *Service* ``selector`` will cause Shipper to error, as it will not be
able to balance traffic between multiple *Releases*. 

If you cannot modify the Chart you're rolling out, you can ask Shipper to
remove the ``release`` selector from the *Service* ``selector`` by adding the
``enable-helm-release-workaround: "true"`` label to your *Application*. This
workaround helps make Charts created with ``helm create`` work out of the box.

**************
Load balancing
**************

Shipper uses Kubernetes' built-in mechanism for shifting traffic: labeling
*Pods* to add or remove them to a *Service's* ``selector``. This means you
don't need any special support in your Kubernetes clusters, but it has several
drawbacks. 

We hope to mitigate these by adding support for service mesh providers as
traffic shifting backends.

Pod-based traffic shifting
--------------------------

Traffic shifting happens at the granularity of *Pods*, not requests. While
Shipper's interface specifes a traffic weight, small fleets of *Pods* may
find that their actual weight differs significantly from the one they
requested.

New *Pods* don't get traffic if Shipper is not working
------------------------------------------------------

Shipper adds the ``shipper-traffic-status: enabled`` label to *Pods*
after they start. This allows Shipper to correctly manage the number
of *Pods* exposed to traffic. However, if a *Pod* is deleted and
Shipper is not currently running on that cluster, the new *Pod*
spawned by the *ReplicaSet* will not get traffic until Shipper is
working again.

The primary issue is that we cannot "cork" a successfully completed rollout by
adding the traffic label to the *Deployment* or *ReplicaSet* without triggering
a native *Deployment*-based rollout.  We could solve this by working directly
with *ReplicaSets* instead of *Deployments*, but that's probably working
against the grain of the ecosystem (most charts contain *Deployments*).

******************
Lock-step rollouts
******************

Shipper is good at making sure that all clusters involved in a rollout are in
the same state. It does this by ensuring that all clusters are in the correct
state before marking a rollout step as complete. 

However, this means that Shipper cannot perform cluster-by-cluster rollouts,
like first ``kube-us-east1-a``, then ``kube-eu-west2-b``. Our "federation"
layer supports this, but we have not yet designed the extension to our strategy
language to describe this kind of rollout.

This cluster-by-cluster strategy is important when limiting traffic or capacity
exposure to a new change is not enough to mitigate risk: for example, perhaps
the new version will change a cluster-local schema once it starts running.
