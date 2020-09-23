module github.com/bookingcom/shipper

go 1.13

require (
	github.com/Masterminds/semver v1.4.0
	github.com/Masterminds/sprig v2.14.1+incompatible // indirect
	github.com/OneOfOne/xxhash v1.2.5 // indirect
	github.com/aokoli/goutils v1.0.1 // indirect
	github.com/cespare/xxhash v1.1.0
	github.com/ghodss/yaml v1.0.0
	github.com/gobwas/glob v0.2.2 // indirect
	github.com/google/go-cmp v0.4.0
	github.com/gophercloud/gophercloud v0.1.0 // indirect
	github.com/huandu/xstrings v0.0.0-20171208101919-37469d0c81a7 // indirect
	github.com/imdario/mergo v0.3.7 // indirect
	github.com/mattn/go-runewidth v0.0.9 // indirect
	github.com/mitchellh/go-homedir v1.1.0
	github.com/pmezard/go-difflib v1.0.0
	github.com/prometheus/client_golang v0.9.3
	github.com/satori/go.uuid v1.2.0 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/spf13/cobra v1.0.0
	github.com/tatsushid/go-prettytable v0.0.0-20141013043238-ed2d14c29939
	k8s.io/api v0.19.2
	k8s.io/apiextensions-apiserver v0.0.0-20190602131520-451a9c13a3c8
	k8s.io/apimachinery v0.19.2
	k8s.io/cli-runtime v0.19.2
	k8s.io/client-go v0.19.2
	k8s.io/code-generator v0.0.0-20190531131525-17d711082421
	k8s.io/helm v2.8.0+incompatible
	k8s.io/klog v0.3.2
	sigs.k8s.io/structured-merge-diff v0.0.0-20190525122527-15d366b2352e // indirect
	sigs.k8s.io/yaml v1.2.0
)

replace k8s.io/apimachinery v0.0.0-20190602113612-63a6072eb563 => k8s.io/apimachinery v0.0.0-20190602183612-63a6072eb563
