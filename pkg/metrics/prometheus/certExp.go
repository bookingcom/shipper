package prometheus

import (
	"time"

	prom "github.com/prometheus/client_golang/prometheus"
)

type TLSCertExpireMetric struct {
	ExpirationGauge *prom.GaugeVec
	HeartbeatGauge  *prom.GaugeVec
}

func NewTLSCertExpireMetric() *TLSCertExpireMetric {
	return &TLSCertExpireMetric{prom.NewGaugeVec(
		prom.GaugeOpts{
			Namespace: ns,
			Subsystem: tlsSubsys,
			Name:      "expire_time_epoch",
			Help:      "When does Shipper Validating Webhooks TLS certificate expire",
		},
		[]string{"host"},
	),
		prom.NewGaugeVec(
			prom.GaugeOpts{
				Namespace: ns,
				Subsystem: tlsSubsys,
				Name:      "heartbeat",
				Help:      "The last time Shipper Validating Webhooks sent a heartbeat",
			},
			[]string{"host"},
		)}
}

func (m *TLSCertExpireMetric) Observe(host string, exp time.Time) {
	m.ExpirationGauge.WithLabelValues(host).Set(float64(exp.Unix()))
}

func (m *TLSCertExpireMetric) ObserveHeartBeat(host string) {
	m.HeartbeatGauge.WithLabelValues(host).SetToCurrentTime()
}

func (m *TLSCertExpireMetric) GetMetrics() []prom.Collector {
	return []prom.Collector{
		m.ExpirationGauge,
		m.HeartbeatGauge,
	}
}
