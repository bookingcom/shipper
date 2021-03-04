module github.com/bookingcom/shipper

go 1.13

require (
	github.com/Masterminds/semver v1.4.0
	github.com/Masterminds/sprig v2.14.1+incompatible // indirect
	github.com/OneOfOne/xxhash v1.2.5 // indirect
	github.com/aokoli/goutils v1.0.1 // indirect
	github.com/cespare/xxhash v1.1.0
	github.com/cyphar/filepath-securejoin v0.2.2 // indirect
	github.com/go-openapi/validate v0.19.5 // indirect
	github.com/google/go-cmp v0.5.2
	github.com/gophercloud/gophercloud v0.1.0 // indirect
	github.com/mitchellh/go-homedir v1.1.0
	github.com/pmezard/go-difflib v1.0.0
	github.com/prometheus/client_golang v1.7.1
	github.com/prometheus/client_model v0.2.0
	github.com/rodaine/table v1.0.1
	github.com/satori/go.uuid v1.2.0 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/spf13/cobra v1.1.1
	github.com/spf13/pflag v1.0.5
	github.com/ugorji/go v1.1.4 // indirect
	gonum.org/v1/netlib v0.0.0-20190331212654-76723241ea4e // indirect
	helm.sh/helm/v3 v3.5.2
	k8s.io/api v0.20.2
	k8s.io/apiextensions-apiserver v0.20.2
	k8s.io/apimachinery v0.20.2
	k8s.io/client-go v0.20.2
	k8s.io/code-generator v0.20.2
	k8s.io/helm v2.16.12+incompatible
	k8s.io/klog v1.0.0
	sigs.k8s.io/structured-merge-diff/v2 v2.0.1 // indirect
	sigs.k8s.io/yaml v1.2.0
)

replace (
	github.com/docker/distribution => github.com/docker/distribution v0.0.0-20191216044856-a8371794149d
	github.com/docker/docker => github.com/moby/moby v1.4.2-0.20200203170920-46ec8731fbce
)
