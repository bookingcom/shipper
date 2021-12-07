## Changelog since v0.8

> You might notice that there is no version 0.9 of Shipper. This is
> because in version 0.9, we tried to split Shipper into two components
> (`shipper-mgmt` and `shipper-app`) which would run in management and
> application clusters respectively. However, that version was behaving
> erratically in a way that was hard to predict and debug. After
> spending months trying to patch all the holes, we decided to forgo the
> separation for now and move on with the development of other features.
>
> Please note that this means that Shipper is still one component,
> running only in the management cluster.

### Breaking Changes

* Shipper now uses different names for its service accounts, roles,
  rolebindings and clusterrolebindings. Refer to the [migrating to Shipper 0.10][] 
  section for more information on how to migrate to
  the new version safely.

### Improvements

* `Shipperctl admin clusters apply` is split into multiple commands,
  so that each operation can be done separately. For example, this
  allows operators to only set up the application clusters, without
  touching the management cluster ([#358][])
* `Shipperctl` will now create an explicit service account for the
  application clusters and not give the management cluster admin rights
  as before. Note that this service account will not have permissions
  to create new namespaces in the application clusters ([#402][]).
* It is now possible to create backups and restore backups using 
  `shipperctl backup` commands ([#372][]).
* It's useful to know what kind of objects shipper is rendering before
  they reach the api server. For debugging issues with YAML
  serializing/deserializing, for instance. 
  It is now possible using `shipperctl chart render` commands ([#374][])
* Shipper now exposes metrics on the health of the webhook. For now,
  that includes the time that the SSL certificate expires, and a secondly
  heartbeat ([#366][])
* Shipperctl now creates and modifies the webhook with the [failure
  policy][] set to `Fail` ([#366][]. This means that the webhook
  becomes a very important piece of the user experience, and we
  suggest you monitor the Shipper webhook's health using the metrics
  mentioned above.
* The manifests now have resources limits and requests. This states 
  the resources Shipper requires, in case there is a resource manager 
  of some sort set in place ([#380][])
* Shipper now exposes metrics that measure time from Application 
  creation/modification to Chart Installation ([#383][])

### Bug fixes
* Shipper now rejects all modifications to the `environment` field of
  all releases. This fixes an issue where users would modify this
  field and cause an unsupported behavior ([#357][])
* Fix dropping pods when moving back in the strategy([#387][])
* Webhook validates deletion. This prevents users to delete release 
  objects when there's a rollout block thus creating an outage for their service ([#392][])
* Fixed a bug in the installation target where a deleted namespace in application cluster 
  related error will never be retried ([#396][]))

### Migrating to 0.10

- Run `shipperctl clusters setup management`, `shipperctl clusters
  join` to create the
  relevant CRDs, service accounts and RBAC objects
- Make sure your context is set to the management cluster, and apply
  the Shipper 0.10 deployment object by doing `kubectl apply
  -f
  https://github.com/bookingcom/shipper/releases/download/v0.10.0/shipper.deployment.v0.10.0.yaml`
  and for shipper-state-metrics `kubectl apply -f
  https://github.com/bookingcom/shipper/releases/download/v0.10.0/shipper-state-metrics.deployment.v0.10.0.yaml`
- Start monitoring the health of the webhook. You can use the
  `shipper_webhook_health_expire_time_epoch` and
  `shipper_webhook_health_heartbeat` Prometheus metrics.
  
### Reverting to 0.8

- Remove the Shipper deployments on management cluster
- Run `shipperctl` 0.8 to revert service accounts and cluster role objects
  to the state that Shipper 0.8
  expects them to be in
- Create the Shipper deployment on the management cluster with the
  relevant image tag, `v0.8.2`

[migrating to Shipper 0.10]: #Migrating-to-0.10
[#358]: https://github.com/bookingcom/shipper/pull/358
[#366]: https://github.com/bookingcom/shipper/pull/366
[#357]: https://github.com/bookingcom/shipper/pull/357
[#372]: https://github.com/bookingcom/shipper/pull/372
[#374]: https://github.com/bookingcom/shipper/pull/374
[#380]: https://github.com/bookingcom/shipper/pull/380
[#383]: https://github.com/bookingcom/shipper/pull/383
[#387]: https://github.com/bookingcom/shipper/pull/387
[#392]: https://github.com/bookingcom/shipper/pull/392
[#396]: https://github.com/bookingcom/shipper/pull/396
[#402]: https://github.com/bookingcom/shipper/pull/402
[failure policy]: https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy
