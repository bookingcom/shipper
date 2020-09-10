package prometheus

import (
	prom "github.com/prometheus/client_golang/prometheus"
)

type EnqueueMetric struct {
	EnqueueRelGauge        *prom.GaugeVec
	EnqueuePerClusterGauge *prom.GaugeVec
}

func NewEnqueueMetric() *EnqueueMetric {
	return &EnqueueMetric{
		EnqueueRelGauge: prom.NewGaugeVec(
			prom.GaugeOpts{
				Namespace: ns,
				Subsystem: wqSubsys,
				Name:      "enqueue_release",
				Help:      "which objects were triggering enqueueing releases",
			},
			[]string{"enqueued_release_name", "triggering_object_name", "triggering_object_kind"},
		),
		EnqueuePerClusterGauge: prom.NewGaugeVec(
			prom.GaugeOpts{
				Namespace: ns,
				Subsystem: wqSubsys,
				Name:      "enqueue_release_per_cluster",
				Help:      "which objects were triggering enqueueing releases per scheduled cluster",
			},
			[]string{"enqueued_release_name", "enqueued_release_cluster", "triggering_object_name", "triggering_object_kind"},
		),
	}
}

func (m *EnqueueMetric) ObserveEnqueueRelease(enqueuedReleaseClusters []string, enqueuedReleaseName, triggeringObjectName, triggeringObjectKind string) {
	m.EnqueueRelGauge.WithLabelValues(enqueuedReleaseName, triggeringObjectName, triggeringObjectKind).Inc()
	for _, clusterName := range enqueuedReleaseClusters {
		m.EnqueuePerClusterGauge.WithLabelValues(enqueuedReleaseName, clusterName, triggeringObjectName, triggeringObjectKind).Inc()
	}
}

func (m *EnqueueMetric) GetMetrics() []prom.Collector {
	return []prom.Collector{
		m.EnqueueRelGauge,
		m.EnqueuePerClusterGauge,
	}
}
