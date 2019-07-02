package main

import (
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	kubelisters "k8s.io/client-go/listers/core/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
)

var (
	appsDesc = prometheus.NewDesc(
		fqn("applications"),
		"Number of Application objects",
		[]string{"namespace"},
		nil,
	)

	relsDesc = prometheus.NewDesc(
		fqn("releases"),
		"Number of Release objects",
		[]string{"namespace", "shipper_app", "cluster", "cond_type", "cond_status", "cond_reason"},
		nil,
	)

	relDurationDesc = prometheus.NewDesc(
		fqn("release_durations"),
		"Duration of release objects",
		[]string{"cond_type"},
		nil,
	)

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

	clustersDesc = prometheus.NewDesc(
		fqn("clusters"),
		"Number of Cluster objects",
		[]string{"name", "schedulable", "has_secret"},
		nil,
	)

	rolloutblocksDesc = prometheus.NewDesc(
		fqn("rolloutblocks"),
		"Number of RolloutBlock objects",
		[]string{"namespace"},
		nil,
	)
)

var everything = labels.Everything()

type ShipperStateMetrics struct {
	appsLister     shipperlisters.ApplicationLister
	relsLister     shipperlisters.ReleaseLister
	itsLister      shipperlisters.InstallationTargetLister
	ctsLister      shipperlisters.CapacityTargetLister
	ttsLister      shipperlisters.TrafficTargetLister
	clustersLister shipperlisters.ClusterLister
	rbLister	   shipperlisters.RolloutBlockLister

	nssLister     kubelisters.NamespaceLister
	secretsLister kubelisters.SecretLister

	shipperNs string
}

func (ssm ShipperStateMetrics) Collect(ch chan<- prometheus.Metric) {
	ssm.collectApplications(ch)
	ssm.collectReleases(ch)
	ssm.collectInstallationTargets(ch)
	ssm.collectCapacityTargets(ch)
	ssm.collectTrafficTargets(ch)
	ssm.collectClusters(ch)
	ssm.collectRolloutBlocks(ch)
}

func (ssm ShipperStateMetrics) Describe(ch chan<- *prometheus.Desc) {
	ch <- appsDesc
	ch <- relsDesc
	ch <- itsDesc
	ch <- ctsDesc
	ch <- ttsDesc
	ch <- clustersDesc
	ch <- rolloutblocksDesc
}

func (ssm ShipperStateMetrics) collectApplications(ch chan<- prometheus.Metric) {
	nss, err := getNamespaces(ssm.nssLister)
	if err != nil {
		glog.Warningf("collect Namespaces: %s", err)
		return
	}

	apps, err := ssm.appsLister.List(everything)
	if err != nil {
		glog.Warningf("collect Applications: %s", err)
		return
	}

	appsPerNamespace := make(map[string]float64)
	for _, app := range apps {
		appsPerNamespace[app.Namespace]++
	}

	glog.V(4).Infof("apps: %v", appsPerNamespace)

	for _, ns := range nss {
		n, ok := appsPerNamespace[ns.Name]
		if !ok {
			n = 0
		}

		ch <- prometheus.MustNewConstMetric(appsDesc, prometheus.GaugeValue, n, ns.Name)
	}
}

func (ssm ShipperStateMetrics) collectReleases(ch chan<- prometheus.Metric) {
	rels, err := ssm.relsLister.List(everything)
	if err != nil {
		glog.Warningf("collect Releases: %s", err)
		return
	}

	key := func(ss ...string) string { return strings.Join(ss, "^") }
	unkey := func(s string) []string { return strings.Split(s, "^") }

	now := time.Now()
	relAgesByCondition := make(map[string][]float64)

	breakdown := make(map[string]float64)
	for _, rel := range rels {
		var appName string
		if len(rel.OwnerReferences) == 1 {
			appName = rel.OwnerReferences[0].Name
		} else {
			appName = "unknown"
		}

		clusters := strings.Split(rel.Annotations[shipper.ReleaseClustersAnnotation], ",")
		if len(clusters) == 0 || len(clusters) == 1 && clusters[0] == "" {
			clusters = []string{"unknown"}
		}

		for _, cluster := range clusters {
			for _, cond := range rel.Status.Conditions {
				reason := cond.Reason
				if reason == "" {
					reason = "NoReason"
				}
				// it's either this or map[string]map[string]map[string]map[string]float64
				breakdown[key(rel.Namespace, appName, cluster, string(cond.Type), string(cond.Status), reason)]++
			}
		}

		if rel.Status.Strategy != nil {
			for _, condition := range rel.Status.Strategy.Conditions {
				if condition.Status == corev1.ConditionFalse {
					continue
				}
				age := now.Sub(condition.LastTransitionTime.Time).Seconds()
				relAgesByCondition[string(condition.Type)] = append(relAgesByCondition[string(condition.Type)], age)
			}
		}

	}

	glog.V(4).Infof("releases: %v", breakdown)

	for k, v := range breakdown {
		ch <- prometheus.MustNewConstMetric(relsDesc, prometheus.GaugeValue, v, unkey(k)...)
	}

	quantiles := []float64{0.5, 0.9, 0.99}

	for condition, ages := range relAgesByCondition {
		count := uint64(len(ages))
		sum := Sum(ages)
		summary, err := MakeSummary(ages, quantiles)
		if err != nil {
			glog.Warningf("collect Releases: %s", err)
			return
		}

		ch <- prometheus.MustNewConstSummary(relDurationDesc, count, sum, summary, condition)
	}
}

func (ssm ShipperStateMetrics) collectInstallationTargets(ch chan<- prometheus.Metric) {
	nss, err := getNamespaces(ssm.nssLister)
	if err != nil {
		glog.Warningf("collect Namespaces: %s", err)
		return
	}

	its, err := ssm.itsLister.List(everything)
	if err != nil {
		glog.Warningf("collect InstallationTargets: %s", err)
		return
	}

	itsPerNamespace := make(map[string]float64)
	for _, it := range its {
		itsPerNamespace[it.Namespace]++
	}

	glog.V(4).Infof("its: %v", itsPerNamespace)

	for _, ns := range nss {
		n, ok := itsPerNamespace[ns.Name]
		if !ok {
			n = 0
		}

		ch <- prometheus.MustNewConstMetric(itsDesc, prometheus.GaugeValue, n, ns.Name)
	}
}

func (ssm ShipperStateMetrics) collectCapacityTargets(ch chan<- prometheus.Metric) {
	nss, err := getNamespaces(ssm.nssLister)
	if err != nil {
		glog.Warningf("collect Namespaces: %s", err)
		return
	}

	cts, err := ssm.ctsLister.List(everything)
	if err != nil {
		glog.Warningf("collect CapacityTargets: %s", err)
		return
	}

	ctsPerNamespace := make(map[string]float64)
	for _, it := range cts {
		ctsPerNamespace[it.Namespace]++
	}

	glog.V(4).Infof("cts: %v", ctsPerNamespace)

	for _, ns := range nss {
		n, ok := ctsPerNamespace[ns.Name]
		if !ok {
			n = 0
		}

		ch <- prometheus.MustNewConstMetric(ctsDesc, prometheus.GaugeValue, n, ns.Name)
	}
}

func (ssm ShipperStateMetrics) collectTrafficTargets(ch chan<- prometheus.Metric) {
	nss, err := getNamespaces(ssm.nssLister)
	if err != nil {
		glog.Warningf("collect Namespaces: %s", err)
		return
	}

	tts, err := ssm.ttsLister.List(everything)
	if err != nil {
		glog.Warningf("collect TrafficTargets: %s", err)
		return
	}

	ttsPerNamespace := make(map[string]float64)
	for _, it := range tts {
		ttsPerNamespace[it.Namespace]++
	}

	glog.V(4).Infof("tts: %v", ttsPerNamespace)

	for _, ns := range nss {
		n, ok := ttsPerNamespace[ns.Name]
		if !ok {
			n = 0
		}

		ch <- prometheus.MustNewConstMetric(ttsDesc, prometheus.GaugeValue, n, ns.Name)
	}
}

func (ssm ShipperStateMetrics) collectClusters(ch chan<- prometheus.Metric) {
	clusters, err := ssm.clustersLister.List(everything)
	if err != nil {
		glog.Warningf("collect Clusters: %s", err)
		return
	}

	for _, cluster := range clusters {
		_, err := ssm.secretsLister.Secrets(ssm.shipperNs).Get(cluster.Name)

		hasSecret := "true"
		if kerrors.IsNotFound(err) {
			hasSecret = "false"
		}

		schedulable := "true"
		if cluster.Spec.Scheduler.Unschedulable {
			schedulable = "false"
		}

		ch <- prometheus.MustNewConstMetric(clustersDesc, prometheus.GaugeValue, 1.0, cluster.Name, schedulable, hasSecret)
	}
}

func (ssm ShipperStateMetrics) collectRolloutBlocks(ch chan<- prometheus.Metric) {
	nss, err := getNamespaces(ssm.nssLister)
	if err != nil {
		glog.Warningf("collect Namespaces: %s", err)
		return
	}

	rolloutBlocks, err := ssm.rbLister.List(everything)
	if err != nil {
		glog.Warningf("collect RolloutBlocks: %s", err)
		return
	}

	rbsPerNamespace := make(map[string]float64)
	for _, rolloutBlock := range rolloutBlocks {
		rbsPerNamespace[rolloutBlock.Namespace]++
	}

	glog.V(4).Infof("RolloutBlocks: %v", rbsPerNamespace)

	for _, ns := range nss {
		n, ok := rbsPerNamespace[ns.Name]
		if !ok {
			n = 0
		}

		ch <- prometheus.MustNewConstMetric(rolloutblocksDesc, prometheus.GaugeValue, n, ns.Name)
	}
}

func fqn(name string) string {
	const (
		ns     = "shipper"
		subsys = "objects"
	)

	return ns + "_" + subsys + "_" + name
}

func getNamespaces(lister kubelisters.NamespaceLister) ([]*corev1.Namespace, error) {
	nss, err := lister.List(everything)
	if err != nil {
		return nil, err
	}

	nsBlacklist := []string{"kube-system", "kube-public", "kube-dns", shipper.ShipperNamespace}

	filtered := make([]*corev1.Namespace, 0, len(nss))
NS:
	for _, ns := range nss {
		for _, black := range nsBlacklist {
			if ns.Name == black {
				continue NS
			}
		}

		filtered = append(filtered, ns)
	}

	return filtered, nil
}
