## Changelog since v0.5.0

* Implemented global and namespace-local rollout blocks [issue](https://github.com/bookingcom/shipper/issues/1) [pull request](https://github.com/bookingcom/shipper/pull/151)
* Implemented self-contained InstallationTarget objects [issue](https://github.com/bookingcom/shipper/issues/5) [pull request](https://github.com/bookingcom/shipper/pull/114) [pull request](https://github.com/bookingcom/shipper/pull/183)
* Fixed a bug in scheduler where a release marked as scheduled:false would never be re-scheduled [issue](https://github.com/bookingcom/shipper/issues/125) [pull request](https://github.com/bookingcom/shipper/pull/126)
* Fixed a bug in Release Controller where under some circumstances an outdated release could be recognised as a contender and become active [pull request](https://github.com/bookingcom/shipper/pull/166)
* Improved Application object representation in `kubectl get` output [pull request](https://github.com/bookingcom/shipper/pull/181)
* Improved internal problem exposure [issue](https://github.com/bookingcom/shipper/issues/170) [pull request](https://github.com/bookingcom/shipper/pull/179) [issue](https://github.com/bookingcom/shipper/issues/123) [pull request](https://github.com/bookingcom/shipper/pull/126) [issue](https://github.com/bookingcom/shipper/issues/174) [pull request](https://github.com/bookingcom/shipper/pull/178) [pull request](https://github.com/bookingcom/shipper/pull/135)
