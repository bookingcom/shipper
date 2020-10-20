.. _developer_building:

Building Shipper yourself
=========================

Shipper uses ``go build`` to produce binaries. All dependencies are present in Shipper's repository, and managed with Go modules.

.. _install_requirements:

Requirements
------------

* Go 1.15+

Building Shipper
----------------

Assuming you have Go installed, after changing into the directory that you'd like to clone Shipper into, run:

.. code-block:: shell
    :caption: build.sh
    :name: shipper_build_sh

    git clone git@github.com:bookingcom/shipper.git
    go build cmd/shipper
