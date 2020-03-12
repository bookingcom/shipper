module github.com/bookingcom/shipper

go 1.13

require (
	cloud.google.com/go v0.39.0 // indirect
	github.com/Masterminds/semver v1.4.0
	github.com/OneOfOne/xxhash v1.2.5 // indirect
	github.com/cespare/xxhash v1.1.0
	github.com/google/go-cmp v0.3.1
	github.com/mitchellh/go-homedir v1.1.0
	github.com/pmezard/go-difflib v1.0.0
	github.com/prometheus/client_golang v1.2.1
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/spf13/cobra v0.0.5
	golang.org/x/exp v0.0.0-20190510132918-efd6b22b2522 // indirect
	helm.sh/helm/v3 v3.0.3
	k8s.io/api v0.0.0-20191016110408-35e52d86657a
	k8s.io/apiextensions-apiserver v0.0.0-20191016113550-5357c4baaf65
	k8s.io/apimachinery v0.0.0-20191004115801-a2eda9f80ab8
	k8s.io/client-go v0.0.0-20191016111102-bec269661e48
	k8s.io/code-generator v0.0.0-20191004115455-8e001e5d1894
	k8s.io/klog v1.0.0
	sigs.k8s.io/yaml v1.1.0
)

replace k8s.io/apimachinery v0.0.0-20190602113612-63a6072eb563 => k8s.io/apimachinery v0.0.0-20190602183612-63a6072eb563

replace github.com/docker/docker => github.com/moby/moby v0.7.3-0.20190826074503-38ab9da00309
