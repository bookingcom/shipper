package prometheus

import (
	"time"

	prom "github.com/prometheus/client_golang/prometheus"
)

type WebhookMetric struct {
	ExpirationGauge *prom.GaugeVec
	HeartbeatGauge  *prom.GaugeVec
}

func NewTLSCertExpireMetric() *WebhookMetric {
	return &WebhookMetric{prom.NewGaugeVec(
		prom.GaugeOpts{
			Namespace: ns,
			Subsystem: webhookSubsys,
			Name:      "expire_time_epoch",
			Help:      "When does Shipper Validating Webhooks TLS certificate expire",
		},
		[]string{"host"},
	),
		prom.NewGaugeVec(
			prom.GaugeOpts{
				Namespace: ns,
				Subsystem: webhookSubsys,
				Name:      "heartbeat",
				Help:      "The last time Shipper Validating Webhooks sent a heartbeat",
			},
			[]string{"host"},
		)}
}

func (m *WebhookMetric) ObserveCertificateExpiration(host string, exp time.Time) {
	m.ExpirationGauge.WithLabelValues(host).Set(float64(exp.Unix()))
}

func (m *WebhookMetric) ObserveHeartBeat(host string) {
	m.HeartbeatGauge.WithLabelValues(host).SetToCurrentTime()
}

func (m *WebhookMetric) GetMetrics() []prom.Collector {
	return []prom.Collector{
		m.ExpirationGauge,
		m.HeartbeatGauge,
	}
}
