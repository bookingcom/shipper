package prometheus

import (
	"net/url"
	"strings"
	"time"

	"github.com/golang/glog"
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
		[]string{"verb", "host", "gvr", "watch"},
	)}
}

func (m *RESTLatencyMetric) Observe(verb string, u url.URL, latency time.Duration) {
	watch := u.Query().Get("watch")
	if watch != "true" {
		watch = "false"
	}

	// TODO(asurikov): see if there's an easy way to substitute u.Host with cluster
	// name?

	gvr := extractGVR(u.Path)

	m.Summary.WithLabelValues(verb, u.Hostname(), gvr, watch).Observe(latency.Seconds())
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

func extractGVR(path string) string {
	gvr := "unknown"

	if len(path) == 0 {
		return gvr
	}

	origPath := path

	if path[0] == '/' {
		path = path[1:]
	}

	pieces := strings.Split(path, "/")
	if len(pieces) < 3 {
		return gvr
	}

	prefix := pieces[0]
	pieces = pieces[1:]

	if prefix == "apis" {
		switch len(pieces) {
		case 2:
			// shipper.booking.com/v1alpha1
			gvr = pieces[0] + "/" + pieces[1] // not a GVR but still useful to see
		case 3:
			// shipper.booking.com/v1alpha1/traffictargets
			gvr = pieces[0] + "/" + pieces[1] + "/" + pieces[2]
		case 5, 6:
			// shipper.booking.com/v1alpha1/namespaces/.../traffictargets
			// shipper.booking.com/v1alpha1/namespaces/.../traffictargets/...
			gvr = pieces[0] + "/" + pieces[1] + "/" + pieces[4]
		}
	} else if prefix == "api" {
		switch len(pieces) {
		case 2:
			// v1/events
			gvr = "core/" + pieces[0] + "/" + pieces[1]
		case 4, 5:
			// v1/namespaces/.../events
			// v1/namespaces/.../events/...
			gvr = "core/" + pieces[0] + "/" + pieces[3]
		}

	}

	glog.V(8).Infof("Parsed API path %q into GVR %q", origPath, gvr)

	return gvr
}
