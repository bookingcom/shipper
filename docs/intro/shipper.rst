#######
Shipper
#######

Shipper is an extension for Kubernetes to add sophisticated rollout strategies
and multi-cluster orchestration.

It lets you use ``kubectl`` to manipulate objects which represent any kind of
rollout strategy, like blue/green or canary. These strategies can deploy to one
cluster, or many clusters across the world.

***********************
Why does Shipper exist?
***********************

Kubernetes is a wonderful platform, but implementing mature rollout strategies
on top of it requires subtle multi-step orchestration: *Deployment* objects are
a building block, not a solution.

When implemented as a set of scripts in CI/CD systems like Jenkins, GitLab, or
Brigade, these strategies can become hard to debug, or leave out important
properties like safe rollbacks.

These problems become more severe when the rollout targets multiple Kubernetes
clusters in multiple regions: the complex, multi-step orchestration has
many opportunities to fail and leave clusters in inconsistent states.

Shipper helps by providing a higher level API for complex rollout strategies to
one or many clusters. It simplifies CI/CD pipeline scripts by letting them
focus on the parts that matter to that particular application.

***********************************************
What is Shipper from a technical point of view?
***********************************************

Shipper is a collection of *Kubernetes controllers* that work with custom
Kubernetes objects to provide a declarative API for advanced rollouts. These
controllers continuously monitor the clusters involved, and converge them on
the declared state. They act as control loops for the different aspects of
a rollout: capacity management, traffic shifting, and Kubernetes object
installation.

For example, you might have a Shipper Application like this:

.. literalinclude:: ../examples/application-minimal.yaml
    :language: yaml

In this example, we're defining an Application named ``reviews-api``. It uses
a Helm Chart of the same name, and deploys to a cluster in the **us-east1**
region. It uses a two step rollout strategy: a basic canary step with a bit of
traffic for the new version, then "all-in". It populates the Helm Chart with
values specifying the image tag.

In order to make this declared state a reality, Shipper will select a matching
cluster, install the Chart objects into that cluster, and with your guidance,
progress through the rollout strategy until the new release is fully live.

****************************************
Multi-cluster, multi-region, multi-cloud
****************************************

Shipper can deploy your application to multiple clusters in different regions.

It expects a Kubernetes API and requires no agent in the application clusters,
so it should work with any compliant Kubernetes implementation like GKE or AKS.
If you can use ``kubectl`` with it, chances are, you can use Shipper with it as
well.

******************
Release Management
******************

Shipper doesn't just copy-paste your code onto multiple clusters for you -- it
allows you to customize the rollout strategy fully. This allows you to craft
a rollout strategy with the appropriate speed/risk balance for your particular
situation.

After each step of the rollout strategy, Shipper pauses to wait for another
update to the *Release* object. This checkpointing approach means that rollouts
are fully declarative, scriptable, and resumable. Shipper can keep a rollout on
a particular step in the strategy for ten seconds or ten hours. At any point
the rollout can be safely aborted, or moved backwards through the strategy to
return to an earlier state.

**********
Roll Backs
**********

Since Shipper keeps a record of all your successful releases, it allows you to
roll back to an earlier release very easily.

***************
Charts As Input
***************

Shipper installs a complete set of Kubernetes objects for a given application.

It does this by relying on `Helm <https://helm.sh>`_, and using Helm Charts as
the unit of configuration deployment. Shipper's Application object provides an
interface for specifying values to a Chart just like the ``helm`` command line
tool.
