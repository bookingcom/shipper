## Changelog since v0.7.0

### Breaking Changes

* A Status subresource was added to the CapacityTarget
  ([#242](https://github.com/bookingcom/shipper/pull/242)) and TrafficTarget
  ([#241](https://github.com/bookingcom/shipper/pull/241)) CRDs. This means
  that updates to these resources will now ignore anything in `.status`. [More
  information is available in the
  documentation.](https://kubernetes.io/docs/tasks/access-kubernetes-api/custom-resources/custom-resource-definitions/#status-subresource).
  If you need to roll back to a previous version of Shipper, it is important to
  run `shipperctl` so that the status subresource is disabled.

### Improvements

* Shipper is now built with Go 1.13.
  [PR #237](https://github.com/bookingcom/shipper/pull/237).

* Shipper is now tested against, and verified to work with Kubernetes v1.15.
  [PR #237](https://github.com/bookingcom/shipper/pull/237).

* InstallationTarget ([#250](https://github.com/bookingcom/shipper/pull/250)),
  CapacityTarget ([#242](https://github.com/bookingcom/shipper/pull/242)) and
  TrafficTarget ([#241](https://github.com/bookingcom/shipper/pull/241))
  objects gained a new `.status.conditions` property. This is what the release
  controller now inspects to decide on the target objects' readiness.

* Capacity controller now reports timeouts and ReplicaSet failures as
  conditions. This makes common mistakes, such as quotas being hit during a new
  release, much easier for users to detect. [PR
  #256](https://github.com/bookingcom/shipper/pull/256).

* Traffic controller now reports pods being labeled, but not making it to
  Endpoints as conditions. This makes another common mistake, invalid Service
  selectors, much easier to detect. [PR
  #258](https://github.com/bookingcom/shipper/pull/258).

* Installation controller now validates a chart before attempting to install
  it. [PR #253](https://github.com/bookingcom/shipper/pull/253).

* Shipper no longer needs access to all Secrets in the cluster, and instead
  just needs access to Secrets in the `shipper-system` namespace. [Issue
  #52](https://github.com/bookingcom/shipper/issues/52) [PR
  #251](https://github.com/bookingcom/shipper/pull/251).

### Bug Fixes

* Release controller now ensures that the state of all Releases are reflected
  into their respective target objects, preventing "rogue pods" from older
  releases from being activated by mistake. [Issue
  #71](https://github.com/bookingcom/shipper/issues/71) [PR
  #254](https://github.com/bookingcom/shipper/pull/254).

* Removing a RolloutBlock now causes all Applications and Releases to have
  their `Blocked` condition set to `False`. [Issue
  #246](https://github.com/bookingcom/shipper/issues/246) [PR
  #247](https://github.com/bookingcom/shipper/pull/247).

* Chart repositories that never resolve no longer block a worker forever. [PR
  #265](https://github.com/bookingcom/shipper/pull/265).

* TrafficTargets no longer check achieved weight against labeled pods, but
  instead pods in Endpoints. [Issue
  #23](https://github.com/bookingcom/shipper/issues/23) [PR
  #238](https://github.com/bookingcom/shipper/pull/238).

* The achieved weight in a TrafficTarget now corresponds to the actual achieved
  weight, instead of just matching the desired weight when it is ready.
  [PR #245](https://github.com/bookingcom/shipper/pull/245).

* Capacity controller no longer considers Pods in Terminating state to be
  excess capacity. [Issue
  #192](https://github.com/bookingcom/shipper/issues/192).
