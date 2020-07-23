package prometheus

import (
	"time"

	prom "github.com/prometheus/client_golang/prometheus"
)

type TLSCertExpireMetric struct {
	Gauge *prom.GaugeVec
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
	)}
}

func (m *TLSCertExpireMetric) Observe(host string, exp time.Time) {
	m.Gauge.WithLabelValues(host).Set(float64(exp.Unix()))
}
