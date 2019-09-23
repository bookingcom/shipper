## Changelog since v0.6.0

### Breaking Changes

* Due to changes in the InstallationTarget CRD
  ([#183](https://github.com/bookingcom/shipper/pull/183)), Shipper v0.6.0 had
  a small piece of code that would perform a data migration automatically the
  first time it ran. In Shipper v0.7.0, that code has been removed, so the
  migration will no longer happen. If you're upgrading Shipper from an earlier
  version, we recommend that you first upgrade to v0.6.0, so your
  InstallationTargets can be migrated properly.
