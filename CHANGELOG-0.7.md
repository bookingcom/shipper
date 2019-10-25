## Changelog since v0.6.0

### Breaking Changes

* Due to changes in the InstallationTarget CRD
  ([#183](https://github.com/bookingcom/shipper/pull/183)), Shipper v0.6.0 had
  a small piece of code that would perform a data migration automatically the
  first time it ran. In Shipper v0.7.0, that code has been removed, so the
  migration will no longer happen. If you're upgrading Shipper from an earlier
  version, we recommend that you first upgrade to v0.6.0, so your
  InstallationTargets can be migrated properly.

## Improvements

* Shipper no longer ships with resyncs enabled by default. This means that we
  no longer reprocess objects over and over again, and as a result a Shipper
  instance managing a lot of applications is a lot faster. [Issue
  #78](https://github.com/bookingcom/shipper/issues/78) [PR
  #213](https://github.com/bookingcom/shipper/pull/213) and too many others to
  list.

* Controllers now emit fewer events in general ([PR
  #222](https://github.com/bookingcom/shipper/pull/222)), and mostly only emit
  events for condition changes ([PR
  #217](https://github.com/bookingcom/shipper/pull/217)).

### Bug Fixes

* Traffic Controller now watches changes to Pod objects to immediately apply
  traffic changes instead of waiting for resyncs. [Issue
  #206](https://github.com/bookingcom/shipper/issues/206) [PR
  #210](https://github.com/bookingcom/shipper/pull/210).

* Installation Controller now watches for Service and Deployment objects being
  deleted, and reinstalls them to prevent Releases being stuck due to those
  objects being missing. [Issue
  #110](https://github.com/bookingcom/shipper/issues/110) [PR
  #205](https://github.com/bookingcom/shipper/pull/205).

* Installation Controller no longer tries to replace objects created by a
  different application. [Issue
  #98](https://github.com/bookingcom/shipper/issues/98) [PR
  #223](https://github.com/bookingcom/shipper/pull/223).

* Installation Controller now knows how to handle multiple objects inside the
  same YAML file in charts. [PR
  #216](https://github.com/bookingcom/shipper/pull/216).
