.. _api-reference_plumbing:

##############
Low-level APIs
##############

These objects represent low-level commands defining the state of specific
clusters, as well as the current status of those commands.  Together they
provide 'just enough federation' to implement Shipper's rollout strategies.

They depend on an associated *Release* object to work correctly: they cannot be
created in isolation.

.. toctree::
    :maxdepth: 2

    installation-target
    capacity-target
    traffic-target
