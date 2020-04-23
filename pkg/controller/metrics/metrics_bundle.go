package metrics

import "github.com/prometheus/client_golang/prometheus"

// MetricsBundle encapsulates all the metrics that the Metrics Controller reports on.
type MetricsBundle struct {
	TimeToInstallation prometheus.Histogram
}

func NewMetricsBundle() *MetricsBundle {
	timeToInstallation := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "Time to installation",
		Help: "The time it takes for a Release to be installed on all of the clusters it's scheduled on",
	})

	return &MetricsBundle{
		TimeToInstallation: timeToInstallation,
	}
}
