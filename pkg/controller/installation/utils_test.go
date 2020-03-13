package installation

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"path"
	"path/filepath"
	"regexp"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/proto/hapi/chart"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

type objectsPerClusterMap map[string][]runtime.Object

var (
	TargetConditionOperational = shipper.TargetCondition{
		Type:   shipper.TargetConditionTypeOperational,
		Status: corev1.ConditionTrue,
	}
	TargetConditionReady = shipper.TargetCondition{
		Type:   shipper.TargetConditionTypeReady,
		Status: corev1.ConditionTrue,
	}

	ClusterInstallationOperational = shipper.ClusterInstallationCondition{
		Type:   shipper.ClusterConditionTypeOperational,
		Status: corev1.ConditionTrue,
	}
	ClusterInstallationReady = shipper.ClusterInstallationCondition{
		Type:   shipper.ClusterConditionTypeReady,
		Status: corev1.ConditionTrue,
	}

	apiResourceList = []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{
					Kind:       "Service",
					Namespaced: true,
					Name:       "services",
				},
			},
		},
		{
			GroupVersion: "apps/v1",
			APIResources: []metav1.APIResource{
				{
					Kind:       "Deployment",
					Namespaced: true,
					Name:       "deployments",
				},
			},
		},
	}
)

var localFetchChart = func(chartspec *shipper.Chart) (*chart.Chart, error) {
	re := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	pathurl := re.ReplaceAllString(chartspec.RepoURL, "_")
	data, err := ioutil.ReadFile(
		path.Join(
			"testdata",
			"chart-cache",
			pathurl,
			fmt.Sprintf("%s-%s.tgz", chartspec.Name, chartspec.Version),
		))
	if err != nil {
		return nil, err
	}
	buf := bytes.NewBuffer(data)
	return chartutil.LoadArchive(buf)
}

func loadService(variant string) *corev1.Service {
	service := &corev1.Service{}
	serviceYamlPath := filepath.Join("testdata", fmt.Sprintf("service-%s.yaml", variant))

	if serviceRaw, err := ioutil.ReadFile(serviceYamlPath); err != nil {
		panic(err)
	} else if _, _, err = scheme.Codecs.UniversalDeserializer().Decode(serviceRaw, nil, service); err != nil {
		panic(err)
	}

	return service
}

func buildDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "reviews-api-reviews-api",
		},
	}
}

// buildCluster returns a cluster.
func buildCluster(name string) *shipper.Cluster {
	return &shipper.Cluster{
		ObjectMeta: v1.ObjectMeta{
			Name: name,
		},
		Status: shipper.ClusterStatus{
			InService: true,
		},
	}
}

func newFixture(objectsPerCluster objectsPerClusterMap) *shippertesting.ControllerTestFixture {
	f := shippertesting.NewControllerTestFixture()

	for clusterName, objs := range objectsPerCluster {
		cluster := f.AddNamedCluster(clusterName)
		cluster.AddMany(objs)
		cluster.InitializeDiscovery(apiResourceList)
		cluster.InitializeDynamicClient(objs)
	}

	return f
}

func buildInstallationTarget(namespace, appName string, clusters []string, chart shipper.Chart) *shipper.InstallationTarget {
	return &shipper.InstallationTarget{
		ObjectMeta: v1.ObjectMeta{
			Name:      appName,
			Namespace: namespace,
			Labels: map[string]string{
				shipper.AppLabel:            appName,
				shipper.HelmWorkaroundLabel: shipper.True,
			},
		},
		Spec: shipper.InstallationTargetSpec{
			Clusters:    clusters,
			Chart:       chart,
			Values:      nil,
			CanOverride: true,
		},
	}
}

func buildChart(appName, version, repoUrl string) shipper.Chart {
	return shipper.Chart{
		Name:    appName,
		Version: version,
		RepoURL: repoUrl,
	}
}

func buildSuccessStatus(clusters []string) shipper.InstallationTargetStatus {
	clusterStatuses := make([]*shipper.ClusterInstallationStatus, 0, len(clusters))

	for _, cluster := range clusters {
		clusterStatuses = append(clusterStatuses, &shipper.ClusterInstallationStatus{
			Name: cluster,
			Conditions: []shipper.ClusterInstallationCondition{
				ClusterInstallationOperational,
				ClusterInstallationReady,
			},
		})
	}

	sort.Sort(byClusterName(clusterStatuses))

	return shipper.InstallationTargetStatus{
		Clusters: clusterStatuses,
		Conditions: []shipper.TargetCondition{
			TargetConditionOperational,
			TargetConditionReady,
		},
	}
}
