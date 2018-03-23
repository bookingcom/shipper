package instrumentedclient

import (
	"context"
	"net/http"
	"net/http/httptrace"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	connDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace: ns,
			Subsystem: subsys,
			Name:      "connection_latency_seconds",
			Help:      "How long it takes to establish a connection for an HTTP request",
		},
		[]string{"network", "addr", "connected"},
	)
)

func instrumentRoundTripperTrace(next http.RoundTripper) promhttp.RoundTripperFunc {
	return promhttp.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var t0 time.Time
		trace := &httptrace.ClientTrace{
			ConnectStart: func(_, _ string) {
				t0 = time.Now()
			},
			ConnectDone: func(network, addr string, err error) {
				t1 := time.Since(t0).Seconds()

				connected := "true"
				if err != nil {
					connected = "false"
				}

				connDuration.WithLabelValues(network, addr, connected).Observe(t1)
			},
		}
		r = r.WithContext(httptrace.WithClientTrace(context.Background(), trace))

		return next.RoundTrip(r)
	})
}
