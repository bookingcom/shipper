package client

import (
	"fmt"
	"runtime"

	apiextensionclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

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

func NewKubeClient(ua string, config *rest.Config) (*kubernetes.Clientset, error) {
	return kubernetes.NewForConfig(decorateConfig(config, ua))
}

func NewKubeClientOrDie(ua string, config *rest.Config) *kubernetes.Clientset {
	return kubernetes.NewForConfigOrDie(decorateConfig(config, ua))
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
