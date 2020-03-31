package client

import (
	"fmt"
	"runtime"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	apiextensionclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	"github.com/bookingcom/shipper/pkg/version"
)

const MainAgent = "shipper"

func decorateConfig(config *rest.Config, ua string) *rest.Config {
	cp := rest.CopyConfig(config)
	// NOTE(btyler): These are deliberately high: we're reasonably certain the
	// API servers can handle a much larger number of requests, and we want to
	// have better sensitivity to any shifts in API call efficiency (as well as
	// give users a better experience by reducing queue latency). I plan to
	// turn this back down once we've got some metrics on where our current ratio
	// of shipper objects to API calls is and we start working towards optimizing
	// that ratio.
	cp.QPS = rest.DefaultQPS * 30
	cp.Burst = rest.DefaultBurst * 30

	platform := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	cp.UserAgent = fmt.Sprintf("%s/%s (%s) %s", MainAgent, version.Version, platform, ua)

	return cp
}

func BuildConfigFromClusterAndSecret(cluster *shipper.Cluster, secret *corev1.Secret) *rest.Config {
	config := &rest.Config{
		Host: cluster.Spec.APIMaster,
	}

	_, tokenOK := secret.Data["token"]
	if tokenOK {
		ca := secret.Data["ca.crt"]
		config.CAData = ca

		token := secret.Data["token"]
		config.BearerToken = string(token)
		return config
	}

	// Let's figure it's either a TLS secret or an opaque thing formatted
	// like a TLS secret.
	if ca, ok := secret.Data["tls.ca"]; ok {
		config.CAData = ca
	}

	if crt, ok := secret.Data["tls.crt"]; ok {
		config.CertData = crt
	}

	if key, ok := secret.Data["tls.key"]; ok {
		config.KeyData = key
	}

	if encodedInsecureSkipTlsVerify, ok := secret.Annotations[shipper.SecretClusterSkipTlsVerifyAnnotation]; ok {
		if insecureSkipTlsVerify, err := strconv.ParseBool(encodedInsecureSkipTlsVerify); err == nil {
			config.Insecure = insecureSkipTlsVerify
		}
	}

	return config
}

func NewKubeClient(ua string, config *rest.Config) (*kubernetes.Clientset, error) {
	return kubernetes.NewForConfig(decorateConfig(config, ua))
}

func NewKubeClientOrDie(ua string, config *rest.Config) *kubernetes.Clientset {
	return kubernetes.NewForConfigOrDie(decorateConfig(config, ua))
}

func NewDynamicClient(ua string, config *rest.Config, gvk *schema.GroupVersionKind) (dynamic.Interface, error) {
	config = decorateConfig(config, ua)
	config.APIPath = dynamic.LegacyAPIPathResolverFunc(*gvk)
	config.GroupVersion = &schema.GroupVersion{Group: gvk.Group, Version: gvk.Version}

	return dynamic.NewForConfig(config)
}

func NewShipperClient(ua string, config *rest.Config) (*shipperclientset.Clientset, error) {
	return shipperclientset.NewForConfig(decorateConfig(config, ua))
}

func NewShipperClientOrDie(ua string, config *rest.Config) *shipperclientset.Clientset {
	return shipperclientset.NewForConfigOrDie(decorateConfig(config, ua))
}

func NewApiExtensionClient(ua string, config *rest.Config) (*apiextensionclientset.Clientset, error) {
	return apiextensionclientset.NewForConfig(decorateConfig(config, ua))
}
