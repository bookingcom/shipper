module github.com/bookingcom/shipper

go 1.13

require (
	cloud.google.com/go v0.39.0 // indirect
	github.com/Masterminds/semver v1.4.0
	github.com/Masterminds/sprig v2.14.1+incompatible // indirect
	github.com/OneOfOne/xxhash v1.2.5 // indirect
	github.com/aokoli/goutils v1.0.1 // indirect
	github.com/cespare/xxhash v1.1.0
	github.com/gobwas/glob v0.2.2 // indirect
	github.com/golang/groupcache v0.0.0-20190129154638-5b532d6fd5ef // indirect
	github.com/google/go-cmp v0.3.0
	github.com/googleapis/gnostic v0.2.0 // indirect
	github.com/huandu/xstrings v0.0.0-20171208101919-37469d0c81a7 // indirect
	github.com/imdario/mergo v0.3.7 // indirect
	github.com/mitchellh/go-homedir v1.0.0
	github.com/pmezard/go-difflib v1.0.0
	github.com/prometheus/client_golang v0.9.3
	github.com/satori/go.uuid v1.2.0 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/spf13/cobra v0.0.3
	golang.org/x/exp v0.0.0-20190510132918-efd6b22b2522 // indirect
	golang.org/x/tools v0.0.0-20190603231351-8aaa1484dc10 // indirect
	google.golang.org/appengine v1.6.0 // indirect
	k8s.io/api v0.17.0
	k8s.io/apiextensions-apiserver v0.0.0-20190602131520-451a9c13a3c8
	k8s.io/apimachinery v0.17.0
	k8s.io/client-go v0.17.0
	k8s.io/code-generator v0.0.0-20190531131525-17d711082421
	k8s.io/gengo v0.0.0-20190327210449-e17681d19d3a // indirect
	k8s.io/helm v2.8.0+incompatible
	k8s.io/klog v1.0.0
	sigs.k8s.io/yaml v1.1.0
)

replace k8s.io/apimachinery v0.0.0-20190602113612-63a6072eb563 => k8s.io/apimachinery v0.0.0-20190602183612-63a6072eb563

replace github.com/Azure/go-autorest => github.com/Azure/go-autorest v12.2.0+incompatible
