package state

import (
	"github.com/prometheus/client_golang/prometheus"
	kubelisters "k8s.io/client-go/listers/core/v1"
	klog "k8s.io/klog"

	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
)

var (
	itsDesc = prometheus.NewDesc(
		fqn("installationtargets"),
		"Number of InstallationTarget objects",
		[]string{"namespace"},
		nil,
	)

	ctsDesc = prometheus.NewDesc(
		fqn("capacitytargets"),
		"Number of CapacityTarget objects",
		[]string{"namespace"},
		nil,
	)

	ttsDesc = prometheus.NewDesc(
		fqn("traffictargets"),
		"Number of TrafficTarget objects",
		[]string{"namespace"},
		nil,
	)
)

type AppMetrics struct {
	ItsLister shipperlisters.InstallationTargetLister
	CtsLister shipperlisters.CapacityTargetLister
	TtsLister shipperlisters.TrafficTargetLister

	NssLister kubelisters.NamespaceLister
}

func (ssm AppMetrics) Collect(ch chan<- prometheus.Metric) {
	ssm.collectInstallationTargets(ch)
	ssm.collectCapacityTargets(ch)
	ssm.collectTrafficTargets(ch)
}

func (ssm AppMetrics) Describe(ch chan<- *prometheus.Desc) {
	ch <- itsDesc
	ch <- ctsDesc
	ch <- ttsDesc
}

func (ssm AppMetrics) collectInstallationTargets(ch chan<- prometheus.Metric) {
	nss, err := getNamespaces(ssm.NssLister)
	if err != nil {
		klog.Warningf("collect Namespaces: %s", err)
		return
	}

	its, err := ssm.ItsLister.List(everything)
	if err != nil {
		klog.Warningf("collect InstallationTargets: %s", err)
		return
	}

	itsPerNamespace := make(map[string]float64)
	for _, it := range its {
		itsPerNamespace[it.Namespace]++
	}

	klog.V(8).Infof("its: %v", itsPerNamespace)

	for _, ns := range nss {
		n, ok := itsPerNamespace[ns.Name]
		if !ok {
			n = 0
		}

		ch <- prometheus.MustNewConstMetric(itsDesc, prometheus.GaugeValue, n, ns.Name)
	}
}

func (ssm AppMetrics) collectCapacityTargets(ch chan<- prometheus.Metric) {
	nss, err := getNamespaces(ssm.NssLister)
	if err != nil {
		klog.Warningf("collect Namespaces: %s", err)
		return
	}

	cts, err := ssm.CtsLister.List(everything)
	if err != nil {
		klog.Warningf("collect CapacityTargets: %s", err)
		return
	}

	ctsPerNamespace := make(map[string]float64)
	for _, it := range cts {
		ctsPerNamespace[it.Namespace]++
	}

	klog.V(8).Infof("cts: %v", ctsPerNamespace)

	for _, ns := range nss {
		n, ok := ctsPerNamespace[ns.Name]
		if !ok {
			n = 0
		}

		ch <- prometheus.MustNewConstMetric(ctsDesc, prometheus.GaugeValue, n, ns.Name)
	}
}

func (ssm AppMetrics) collectTrafficTargets(ch chan<- prometheus.Metric) {
	nss, err := getNamespaces(ssm.NssLister)
	if err != nil {
		klog.Warningf("collect Namespaces: %s", err)
		return
	}

	tts, err := ssm.TtsLister.List(everything)
	if err != nil {
		klog.Warningf("collect TrafficTargets: %s", err)
		return
	}

	ttsPerNamespace := make(map[string]float64)
	for _, it := range tts {
		ttsPerNamespace[it.Namespace]++
	}

	klog.V(8).Infof("tts: %v", ttsPerNamespace)

	for _, ns := range nss {
		n, ok := ttsPerNamespace[ns.Name]
		if !ok {
			n = 0
		}

		ch <- prometheus.MustNewConstMetric(ttsDesc, prometheus.GaugeValue, n, ns.Name)
	}
}
