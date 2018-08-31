// Package instrumentedclient is a wrapper around standard http.Client that
// tracks basic HTTP metrics using Prometheus. It also sets some sensinble
// defaults to important timeout settings.
// All instrumentedclient instances log to the same Prometheus variables, even
// if created with NewClient().
package instrumentedclient

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// Timeout for an entire HTTP transaction: sending request, reading and
	// response, redirects, etc.
	HTTPRequestResponseTimeout = 10 * time.Second
	// Timeout for establishing a TCP connection with a remote end.
	TCPConnectionTimeout = 3 * time.Second
	// TCP keepalive timeout.
	TCPKeepAliveTime = 30 * time.Second
)

const (
	ns     = "shipper"
	subsys = "instrumented_client"
)

var (
	reqCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: ns,
			Subsystem: subsys,
			Name:      "requests_total",
			Help:      "How many requests were done by the instrumentedclient library",
		},
		[]string{"code", "method"},
	)
	reqDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace: ns,
			Subsystem: subsys,
			Name:      "latency_seconds",
			Help:      "How long it takes for the instrumentedclient library to do a request",
		},
		[]string{"code", "method"},
	)

	// Mostly copy-pasted from http.DefaultTransport but with some adjustements.
	httpTransport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   TCPConnectionTimeout,
			KeepAlive: TCPKeepAliveTime,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	roundTripper = promhttp.InstrumentRoundTripperCounter(
		reqCounter,
		promhttp.InstrumentRoundTripperDuration(
			reqDuration,
			instrumentRoundTripperTrace(httpTransport),
		),
	)
)

// DefaultClient is an instrumented http.Client with pre-set timeouts.
var DefaultClient = &http.Client{
	Transport: roundTripper,
	Timeout:   HTTPRequestResponseTimeout,
}

// NewClient returns a new instrumented http.Client with pre-set timeouts. Use
// it if you want to adjust default timeouts.
func NewClient() *http.Client {
	return &http.Client{
		Transport: roundTripper,
		Timeout:   TCPConnectionTimeout,
	}
}

// Get issues a GET request using DefaultClient.
func Get(url string) (*http.Response, error) {
	return DefaultClient.Get(url)
}

// Get issues a GET request using DefaultClient.
func Head(url string) (*http.Response, error) {
	return DefaultClient.Head(url)
}

// Get issues a POST request using DefaultClient.
func Post(url string, contentType string, body io.Reader) (*http.Response, error) {
	return DefaultClient.Post(url, contentType, body)
}

// Get issues a POST (application/x-www-form-urlencoded) request using
// DefaultClient.
func PostForm(url string, data url.Values) (*http.Response, error) {
	return DefaultClient.PostForm(url, data)
}

// GetMetrics returns Prometheus all variables that track metrics. Used for
// registering with an HTTP handler.
func GetMetrics() []prometheus.Collector {
	return []prometheus.Collector{
		reqCounter,
		reqDuration,
		connDuration,
	}
}
