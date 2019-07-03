package prometheus

import (
	"fmt"
	"sync"

	prom "github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/util/workqueue"
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
		Help:      fmt.Sprintf("The number of items waiting to be processed in the %q workqueue", name),
	})

	p.mu.Lock()
	defer p.mu.Unlock()

	p.registry = append(p.registry, metric)

	return metric
}

func (p *PrometheusWorkqueueProvider) NewDeprecatedDepthMetric(name string) workqueue.GaugeMetric {
	metric := prom.NewGauge(prom.GaugeOpts{
		Namespace: ns,
		Subsystem: name + "_" + wqSubsys,
		Name:      "depth_deprecated",
		Help:      fmt.Sprintf("(deprecated) The number of items waiting to be processed in the %q workqueue", name),
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
		Help:      fmt.Sprintf("the number of items in total added to the %q workqueue", name),
	})

	p.mu.Lock()
	defer p.mu.Unlock()

	p.registry = append(p.registry, metric)

	return metric
}

func (p *PrometheusWorkqueueProvider) NewDeprecatedAddsMetric(name string) workqueue.CounterMetric {
	metric := prom.NewCounter(prom.CounterOpts{
		Namespace: ns,
		Subsystem: name + "_" + wqSubsys,
		Name:      "adds_deprecated",
		Help:      fmt.Sprintf("(deprecated) The number of items in total added to the %q workqueue", name),
	})

	p.mu.Lock()
	defer p.mu.Unlock()

	p.registry = append(p.registry, metric)

	return metric
}

func (p *PrometheusWorkqueueProvider) NewLatencyMetric(name string) workqueue.HistogramMetric {
	metric := prom.NewHistogram(prom.HistogramOpts{
		Namespace: ns,
		Subsystem: name + "_" + wqSubsys,
		Name:      "latency_seconds",
		Help:      fmt.Sprintf("How long items stay in the %q workqueue before getting picked up", name),
	})

	p.mu.Lock()
	defer p.mu.Unlock()

	p.registry = append(p.registry, metric)

	return metric
}

func (p *PrometheusWorkqueueProvider) NewDeprecatedLatencyMetric(name string) workqueue.SummaryMetric {
	metric := prom.NewSummary(prom.SummaryOpts{
		Namespace: ns,
		Subsystem: name + "_" + wqSubsys,
		Name:      "latency_microseconds",
		Help:      fmt.Sprintf("How long items stay in the %q workqueue before getting picked up", name),
	})

	p.mu.Lock()
	defer p.mu.Unlock()

	p.registry = append(p.registry, metric)

	return metric
}

func (p *PrometheusWorkqueueProvider) NewWorkDurationMetric(name string) workqueue.HistogramMetric {
	metric := prom.NewHistogram(prom.HistogramOpts{
		Namespace: ns,
		Subsystem: name + "_" + wqSubsys,
		Name:      "duration_seconds",
		Help:      fmt.Sprintf("How long it takes to process items from the %q workqueue", name),
	})

	p.mu.Lock()
	defer p.mu.Unlock()

	p.registry = append(p.registry, metric)

	return metric
}

func (p *PrometheusWorkqueueProvider) NewDeprecatedWorkDurationMetric(name string) workqueue.SummaryMetric {
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
		Help:      fmt.Sprintf("The number of times items from the %q workqueue had to be retried", name),
	})

	p.mu.Lock()
	defer p.mu.Unlock()

	p.registry = append(p.registry, metric)

	return metric
}

func (p *PrometheusWorkqueueProvider) NewDeprecatedRetriesMetric(name string) workqueue.CounterMetric {
	metric := prom.NewCounter(prom.CounterOpts{
		Namespace: ns,
		Subsystem: name + "_" + wqSubsys,
		Name:      "retries_deprecated",
		Help:      fmt.Sprintf("(deprecated) The number of times items from the %q workqueue had to be retried", name),
	})

	p.mu.Lock()
	defer p.mu.Unlock()

	p.registry = append(p.registry, metric)

	return metric
}

func (p *PrometheusWorkqueueProvider) NewLongestRunningProcessorSecondsMetric(name string) workqueue.SettableGaugeMetric {
	metric := prom.NewGauge(prom.GaugeOpts{
		Namespace: ns,
		Subsystem: name + "_" + wqSubsys,
		Name:      "longest_running_processor_seconds",
		Help:      fmt.Sprintf("The longest processed item in the %q queue, in seconds", name),
	})

	p.mu.Lock()
	defer p.mu.Unlock()

	p.registry = append(p.registry, metric)

	return metric
}

func (p *PrometheusWorkqueueProvider) NewDeprecatedLongestRunningProcessorMicrosecondsMetric(name string) workqueue.SettableGaugeMetric {
	metric := prom.NewGauge(prom.GaugeOpts{
		Namespace: ns,
		Subsystem: name + "_" + wqSubsys,
		Name:      "longest_running_processor_microseconds",
		Help:      fmt.Sprintf("The longest processed item in the %q queue, in microseconds", name),
	})

	p.mu.Lock()
	defer p.mu.Unlock()

	p.registry = append(p.registry, metric)

	return metric
}

func (p *PrometheusWorkqueueProvider) NewUnfinishedWorkSecondsMetric(name string) workqueue.SettableGaugeMetric {
	metric := prom.NewGauge(prom.GaugeOpts{
		Namespace: ns,
		Subsystem: name + "_" + wqSubsys,
		Name:      "unfinished_work_seconds",
		Help:      fmt.Sprintf("The total number of seconds that all items from the %q queue have been processed", name),
	})

	p.mu.Lock()
	defer p.mu.Unlock()

	p.registry = append(p.registry, metric)

	return metric
}

func (p *PrometheusWorkqueueProvider) NewDeprecatedUnfinishedWorkSecondsMetric(name string) workqueue.SettableGaugeMetric {
	metric := prom.NewGauge(prom.GaugeOpts{
		Namespace: ns,
		Subsystem: name + "_" + wqSubsys,
		Name:      "unfinished_work_seconds_deprecated",
		Help:      fmt.Sprintf("(deprecated) The total number of seconds that all items from the %q queue have been processed", name),
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
