package prometheus

import (
	"net/url"
	"strings"
	"time"

	prom "github.com/prometheus/client_golang/prometheus"
)

type RESTLatencyMetric struct {
	Summary *prom.SummaryVec
}

func NewRESTLatencyMetric() *RESTLatencyMetric {
	return &RESTLatencyMetric{prom.NewSummaryVec(
		prom.SummaryOpts{
			Namespace: ns,
			Subsystem: restSubsys,
			Name:      "latency_seconds",
			Help:      "How long it takes for the Kubernetes REST client to do a request",
		},
		[]string{"verb", "url", "watch"},
	)}
}

func (m *RESTLatencyMetric) Observe(verb string, u url.URL, latency time.Duration) {
	watch := u.Query().Get("watch")
	if watch != "true" {
		watch = "false"
	}

	// Completely remove things like '?resourceVersion=42'.
	u.RawQuery = ""

	// The REST client replaces some unbounded values with an incovenient
	// placeholder that needs to be URL-encoded.
	u.Path = strings.Replace(u.Path, "{namespace}", "...", -1)
	u.Path = strings.Replace(u.Path, "{name}", "...", -1)

	// It's always HTTPS, we know that.
	u.Scheme = ""

	// XXX see if there's an easy way to substitute u.Host with cluster name?

	m.Summary.WithLabelValues(verb, u.String(), watch).Observe(latency.Seconds())
}

type RESTResultMetric struct {
	Counter *prom.CounterVec
}

func NewRESTResultMetric() *RESTResultMetric {
	return &RESTResultMetric{prom.NewCounterVec(
		prom.CounterOpts{
			Namespace: ns,
			Subsystem: restSubsys,
			Name:      "requests_total",
			Help:      "How many requests were done by the Kubernetes REST client",
		},
		[]string{"code", "method", "host"},
	)}
}

func (m *RESTResultMetric) Increment(code string, method string, host string) {
	m.Counter.WithLabelValues(code, method, host).Inc()
}
