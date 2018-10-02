# Charts

## Requirements

For Shipper to interact correctly with your application, your
kubernetes chart should meet some requirements.

- You must use _Deployments_ `apps/v1`
- You must have only one deployment per chart. Shipper uses the
  `replicaCount` on your deployment to adjust capacity, and it can
  only work with one deployment
- You must have only one service with a label of `shipper-lb:
  production`. This is the service that Shipper manipulates to give or
  take away traffic from a release


- Introduce Helm. Stick to the parts that include its templating abilities, and how it helps the user construct Kubernetes objects
- Take the user through the following:
  - creating a chart
  - Validating it
  - Packaging it
  - Uploading it to our instance of chart museum
- Point the user toward the [helm chart template guide][] to learn more about the syntax

[helm chart template guide]: https://docs.helm.sh/chart_template_guide/#the-chart-template-developer-s-guide
