You might notice that there is no version 0.9 of Shipper. This is
because in version 0.9, we tried to split Shipper into two components
(`shipper-mgmt` and `shipper-app`) which would run in management and
application clusters respectively. However, that version was behaving
erratically in a way that was hard to predict and debug. After
spending months trying to patch all the holes, we decided to forgo the
separation for now and move on with the development of other features.

Please note that this means that Shipper is still one component,
running only in the management cluster.

## Changelog since v0.8.We are skipping version 0.9 because

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
* Shipper now rejects all modifications to the `environment` field of
  all releases. This fixes an issue where users would modify this
  field and cause an unsupported behavior ([#357][])
* Shipper now exposes metrics on the health of the webhook. For now,
  that includes the time that the SSL certificate expires, and a secondly
  heartbeat ([#366][])
* Shipperctl now creates and modifies the webhook with the [failure
  policy][] set to `Fail` ([#366][]. This means that the webhook
  becomes a very important piece of the user experience, and we
  suggest you monitor the Shipper webhook's health using the metrics
  mentioned above.

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
[failure policy]: https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy
