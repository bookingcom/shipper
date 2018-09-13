# On the Management Cluster

- Set up chart museum
- Create shipper-system namespace
- Register CRDs
- Create a role to manage Shipper resources
- Create a clusterrolebinding on the management cluster
- Create a deployment for Shipper

# On the Application Cluster

- Create a shipper-system namespace
- Create a service account in that namespace
- Create a rolebinding for that service account
- Create a clusterrolebinding for the role
- Get the secret from the shipper-system namespace for that account, and apply it to the management cluster. Name is cluster name, namespace is shipper-system, and the secret type is opaque., and annotate it with the checksum
- Create the cluster with the cluster info
