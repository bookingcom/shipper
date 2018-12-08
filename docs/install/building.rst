.. _building:

Building Shipper yourself
=========================

Shipper uses ``go build`` to produce binaries. All dependencies are present in Shipper's repository, and managed with `dep <https://github.com/golang/dep>`_.

.. _install_requirements:

Requirements
------------

* Go 1.10
* Go dep

Building Shipper
----------------

Assuming you have Go installed and the *GOPATH* environment variable properly defined:

.. code-block:: shell
    :caption: build.sh
    :name: shipper_build_sh

    mkdir $GOPATH/src/github.com/bookingcom
    cd $GOPATH/src/github.com/bookingcom
    git clone git@github.com:bookingcom/shipper.git
    go build cmd/shipper