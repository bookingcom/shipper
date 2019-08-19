package client

import (
	"fmt"
	"runtime"
	"time"

	apiextensionclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	"github.com/bookingcom/shipper/pkg/version"
)

const MainAgent = "shipper"

func buildConfig(config rest.Config, ua string, timeout *time.Duration) *rest.Config {
	// NOTE(btyler): These are deliberately high: we're reasonably certain the
	// API servers can handle a much larger number of requests, and we want to
	// have better sensitivity to any shifts in API call efficiency (as well as
	// give users a better experience by reducing queue latency). I plan to
	// turn this back down once we've got some metrics on where our current ratio
	// of shipper objects to API calls is and we start working towards optimizing
	// that ratio.
	config.QPS = rest.DefaultQPS * 30
	config.Burst = rest.DefaultBurst * 30

	if timeout != nil {
		config.Timeout = *timeout
	}

	platform := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	config.UserAgent = fmt.Sprintf("%s/%s (%s) %s", MainAgent, version.Version, platform, ua)

	return &config
}

func NewKubeClient(c *rest.Config, ua string, timeout *time.Duration) (*kubernetes.Clientset, error) {
	return kubernetes.NewForConfig(buildConfig(*c, ua, timeout))
}

func NewKubeClientOrDie(c *rest.Config, ua string, timeout *time.Duration) *kubernetes.Clientset {
	return kubernetes.NewForConfigOrDie(buildConfig(*c, ua, timeout))
}

func NewShipperClient(c *rest.Config, ua string, timeout *time.Duration) (*shipperclientset.Clientset, error) {
	return shipperclientset.NewForConfig(buildConfig(*c, ua, timeout))
}

func NewShipperClientOrDie(c *rest.Config, ua string, timeout *time.Duration) *shipperclientset.Clientset {
	return shipperclientset.NewForConfigOrDie(buildConfig(*c, ua, timeout))
}

func NewApiExtensionClient(c *rest.Config, ua string, timeout *time.Duration) (*apiextensionclientset.Clientset, error) {
	return apiextensionclientset.NewForConfig(buildConfig(*c, ua, timeout))
}
