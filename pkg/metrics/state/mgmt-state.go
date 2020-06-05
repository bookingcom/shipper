package state

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	kubelisters "k8s.io/client-go/listers/core/v1"
	klog "k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
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
		[]string{"namespace", "shipper_app", "cond_type", "cond_status", "cond_reason"},
		nil,
	)

	relsPerClusterDesc = prometheus.NewDesc(
		fqn("releases_per_cluster"),
		"Number of Release objects per cluster",
		[]string{"cluster"},
		nil,
	)

	relDurationDesc = prometheus.NewDesc(
		fqn("release_durations"),
		"Duration of release objects",
		[]string{"cond_type"},
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

type MgmtMetrics struct {
	AppsLister     shipperlisters.ApplicationLister
	RelsLister     shipperlisters.ReleaseLister
	ClustersLister shipperlisters.ClusterLister
	RbLister       shipperlisters.RolloutBlockLister

	NssLister     kubelisters.NamespaceLister
	SecretsLister kubelisters.SecretLister

	ShipperNs string

	ReleaseDurationBuckets []float64
}

func (ssm MgmtMetrics) Collect(ch chan<- prometheus.Metric) {
	ssm.collectApplications(ch)
	ssm.collectReleases(ch)
	ssm.collectClusters(ch)
	ssm.collectRolloutBlocks(ch)
}

func (ssm MgmtMetrics) Describe(ch chan<- *prometheus.Desc) {
	ch <- appsDesc
	ch <- relsDesc
	ch <- clustersDesc
	ch <- rolloutblocksDesc
}

func (ssm MgmtMetrics) collectApplications(ch chan<- prometheus.Metric) {
	nss, err := getNamespaces(ssm.NssLister)
	if err != nil {
		klog.Warningf("collect Namespaces: %s", err)
		return
	}

	apps, err := ssm.AppsLister.List(everything)
	if err != nil {
		klog.Warningf("collect Applications: %s", err)
		return
	}

	appsPerNamespace := make(map[string]float64)
	for _, app := range apps {
		appsPerNamespace[app.Namespace]++
	}

	klog.V(4).Infof("apps: %v", appsPerNamespace)

	for _, ns := range nss {
		n, ok := appsPerNamespace[ns.Name]
		if !ok {
			n = 0
		}

		ch <- prometheus.MustNewConstMetric(appsDesc, prometheus.GaugeValue, n, ns.Name)
	}
}

func (ssm MgmtMetrics) collectReleases(ch chan<- prometheus.Metric) {
	rels, err := ssm.RelsLister.List(everything)
	if err != nil {
		klog.Warningf("collect Releases: %s", err)
		return
	}

	key := func(ss ...string) string { return strings.Join(ss, "^") }
	unkey := func(s string) []string { return strings.Split(s, "^") }

	relAgesByCondition := make(map[string][]float64)

	releasesPerCluster := make(map[string]float64)
	releasesPerCondition := make(map[string]float64)
	conditions := []shipper.ReleaseConditionType{
		shipper.ReleaseConditionTypeComplete,
		shipper.ReleaseConditionTypeBlocked,
	}

	for _, rel := range rels {
		var appName string
		if len(rel.OwnerReferences) == 1 {
			appName = rel.OwnerReferences[0].Name
		} else {
			appName = "unknown"
		}

		clusters := releaseutil.GetSelectedClusters(rel)
		for _, cluster := range clusters {
			releasesPerCluster[cluster]++
		}

		for _, c := range conditions {
			var reason, status string

			cond := releaseutil.GetReleaseCondition(rel.Status, c)

			if cond != nil {
				reason = cond.Reason
				status = string(cond.Status)
			} else {
				reason = "NoReason"
				status = "False"
			}

			if reason == "" {
				reason = "NoReason"
			}

			// it's either this or map[string]map[string]map[string]map[string]float64
			releasesPerCondition[key(rel.Namespace, appName, string(c), status, reason)]++
		}
	}

	for k, v := range releasesPerCondition {
		ch <- prometheus.MustNewConstMetric(relsDesc, prometheus.GaugeValue, v, unkey(k)...)
	}

	for cluster, v := range releasesPerCluster {
		ch <- prometheus.MustNewConstMetric(relsPerClusterDesc, prometheus.GaugeValue, v, cluster)
	}

	for condition, ages := range relAgesByCondition {
		count := uint64(len(ages))
		sum := Sum(ages)
		histogram := MakeHistogram(ages, ssm.ReleaseDurationBuckets)

		ch <- prometheus.MustNewConstHistogram(relDurationDesc, count,
			sum, histogram, condition)
	}
}

func (ssm MgmtMetrics) collectClusters(ch chan<- prometheus.Metric) {
	clusters, err := ssm.ClustersLister.List(everything)
	if err != nil {
		klog.Warningf("collect Clusters: %s", err)
		return
	}

	for _, cluster := range clusters {
		_, err := ssm.SecretsLister.Secrets(ssm.ShipperNs).Get(cluster.Name)

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

func (ssm MgmtMetrics) collectRolloutBlocks(ch chan<- prometheus.Metric) {
	nss, err := getNamespaces(ssm.NssLister)
	if err != nil {
		klog.Errorf("collect Namespaces: %s", err)
		return
	}

	rolloutBlocks, err := ssm.RbLister.List(everything)
	if err != nil {
		klog.Errorf("collect RolloutBlocks: %s", err)
		return
	}

	rbsPerNamespace := make(map[string]float64)
	for _, rolloutBlock := range rolloutBlocks {
		rbsPerNamespace[rolloutBlock.Namespace]++
	}

	klog.V(4).Infof("RolloutBlocks: %v", rbsPerNamespace)

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

	// we filter out these namespaces when gathering metrics,
	// since these namespaces should not hold any shipper applications
	nsBlacklist := []string{"kube-system", "kube-public", "kube-dns", shipper.ShipperNamespace, shipper.GlobalRolloutBlockNamespace}

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
