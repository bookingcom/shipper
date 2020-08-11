## Changelog since v0.8.0

This release makes Shipper run on management and application clusters
both, so that Shipper can still enforce the state in case of loss of
communication between the management and application clusters
([#272][]).

### Breaking Changes

* All target objects were moved to the application clusters
  ([#277][]). Shipper will migrate your target objects to the
  application clusters automatically when it finds them ([#320][]),
  but it won't clean up the original copies in the management
  cluster. This is done so that reverting to 0.8 would be painless.

### improvements

* `Shipperctl admin clusters apply` is split into multiple commands,
  so that each operation can be done separately. For example, this
  allows operators to only set up the application clusters, without
  touching the management cluster ([#292][])
* The fleet summary has been removed from the CapacityTarget because
  it's not useful now that the target objects are in the application
  clusters. Instead, we've made the status of the Release object more
  useful ([#288][])
* Shipper now rejects all modifications to the `environment` field of
  all releases. This fixes an issue where users would modify this
  field and cause an unsupported behavior ([#340][])
* Shipper now exposes metrics on the health of the webhook. For now,
  that means the time that the SSL certificate expires, and a secondly
  heartbeat ([#338][])
* Shipperctl now creates and modifies the webhook with the [failure
  policy][] set to `fail` ([#338][]. This means that the webhook
  becomes a very important piece of the user experience, and we
  suggest you monitor the Shipper webhook's health using the metrics
  mentioned above.

### Migrating to 0.9

- Run `shipperctl clusters setup management`, `shipperctl clusters
  join` and `shipperctl clusters setup application` to create the
  relevant CRDs, service accounts and RBAC objects
- Make sure your context is set to the management cluster, and apply
  the Shipper 0.9 management deployment object by doing `kubectl apply
  -f
  https://github.com/bookingcom/shipper/releases/download/v0.9.0/shipper-mgmt.deployment.v0.9.0.yaml`
- Give Shipper some time to migrate your target objects. You can check
  the progress by looking at the `shipper-mgmt` logs (that is, the
  Shipper instance on the management cluster). You can also use
  `kubectl` to query for target objects which have the
  `shipper-target-object-migration-0.9-completed` label set to
  `"true"`
- Switch your kubectl context to each of the application clusters, and
  apply the Shipper application deployment by doing `kubectl apply -f
  https://github.com/bookingcom/shipper/releases/download/v0.9.0/shipper-app.deployment.v0.9.0.yaml`
- Once you've run Shipper for a while and are certain that you don't
  want to revert to an earlier version, clean up the target objects
  from the management cluster by switching to the management cluster
  context and running `kubectl delete it --all
  --all-namespaces`,`kubectl delete ct --all --all-namespaces` and
  `kubectl delete tt --all --all-namespaces`
- Start monitoring the health of the webhook. You can use the
  `shipper_webhook_health_expire_time_epoch` and
  `shipper_webhook_health_heartbeat` Prometheus metrics.

### Reverting to 0.8

- Remove the Shipper deployments on management and application
  clusters
- Run `shipperctl` 0.8 to revert CRDs to the state that Shipper 0.8
  expects them to be in
- Create the Shipper deployment on the management cluster with the
  relevant image tag, `v0.8.2`

[#272]: https://github.com/bookingcom/shipper/issues/272
[#277]: https://github.com/bookingcom/shipper/issues/277
[#288]: https://github.com/bookingcom/shipper/pull/288
[#292]: https://github.com/bookingcom/shipper/pull/292
[#320]: https://github.com/bookingcom/shipper/pull/320
[#338]: https://github.com/bookingcom/shipper/pull/338
[#340]: https://github.com/bookingcom/shipper/pull/340
[failure policy]: https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy
