package prometheus

import (
	"fmt"
	"sync"

	"k8s.io/client-go/util/workqueue"

	prom "github.com/prometheus/client_golang/prometheus"
)

type PrometheusWorkqueueProvider struct {
	mu       *sync.RWMutex
	registry []prom.Collector
}

func NewProvider() *PrometheusWorkqueueProvider {
	return &PrometheusWorkqueueProvider{
		mu: &sync.RWMutex{},
	}
}

func (p *PrometheusWorkqueueProvider) NewDepthMetric(name string) workqueue.GaugeMetric {
	metric := prom.NewGauge(prom.GaugeOpts{
		Namespace: ns,
		Subsystem: name + "_" + wqSubsys,
		Name:      "depth",
		Help:      fmt.Sprintf("How many items are waiting to be processed in the %q workqueue", name),
	})

	p.mu.Lock()
	defer p.mu.Unlock()

	p.registry = append(p.registry, metric)

	return metric
}

func (p *PrometheusWorkqueueProvider) NewAddsMetric(name string) workqueue.CounterMetric {
	metric := prom.NewCounter(prom.CounterOpts{
		Namespace: ns,
		Subsystem: name + "_" + wqSubsys,
		Name:      "adds",
		Help:      fmt.Sprintf("How many items in total were added to the %q workqueue", name),
	})

	p.mu.Lock()
	defer p.mu.Unlock()

	p.registry = append(p.registry, metric)

	return metric
}

func (p *PrometheusWorkqueueProvider) NewLatencyMetric(name string) workqueue.SummaryMetric {
	metric := prom.NewSummary(prom.SummaryOpts{
		Namespace: ns,
		Subsystem: name + "_" + wqSubsys,
		Name:      "latency_microseconds",
		Help:      fmt.Sprintf("How long items stay in the %q workqueue before being taken for processing", name),
	})

	p.mu.Lock()
	defer p.mu.Unlock()

	p.registry = append(p.registry, metric)

	return metric
}

func (p *PrometheusWorkqueueProvider) NewWorkDurationMetric(name string) workqueue.SummaryMetric {
	metric := prom.NewSummary(prom.SummaryOpts{
		Namespace: ns,
		Subsystem: name + "_" + wqSubsys,
		Name:      "duration_microseconds",
		Help:      fmt.Sprintf("How long it takes to process items from the %q workqueue", name),
	})

	p.mu.Lock()
	defer p.mu.Unlock()

	p.registry = append(p.registry, metric)

	return metric
}

func (p *PrometheusWorkqueueProvider) NewRetriesMetric(name string) workqueue.CounterMetric {
	metric := prom.NewCounter(prom.CounterOpts{
		Namespace: ns,
		Subsystem: name + "_" + wqSubsys,
		Name:      "retries",
		Help:      fmt.Sprintf("How many times items from the %q workqueue had to be retried", name),
	})

	p.mu.Lock()
	defer p.mu.Unlock()

	p.registry = append(p.registry, metric)

	return metric
}

func (p *PrometheusWorkqueueProvider) GetMetrics() []prom.Collector {
	p.mu.RLock()
	defer p.mu.RUnlock()

	metrics := make([]prom.Collector, len(p.registry))
	copy(metrics, p.registry)

	return metrics
}
