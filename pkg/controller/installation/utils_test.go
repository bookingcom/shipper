package installation

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

type objectsPerClusterMap map[string][]runtime.Object

const (
	// charts need to match a file in "testdata/$chartName-$version.tar.gz"
	nginxChartName   = "nginx"
	reviewsChartName = "reviews-api"
)

var (
	ClusterInstallationOperational = shipper.ClusterInstallationCondition{
		Type:   shipper.ClusterConditionTypeOperational,
		Status: corev1.ConditionTrue,
	}
	ClusterInstallationReady = shipper.ClusterInstallationCondition{
		Type:   shipper.ClusterConditionTypeReady,
		Status: corev1.ConditionTrue,
	}
	TargetConditionOperational = shipper.TargetCondition{
		Type:   shipper.TargetConditionTypeOperational,
		Status: corev1.ConditionTrue,
	}
	TargetConditionReady = shipper.TargetCondition{
		Type:   shipper.TargetConditionTypeReady,
		Status: corev1.ConditionTrue,
	}
	TargetConditionReadyUnknown = shipper.TargetCondition{
		Type:   shipper.TargetConditionTypeReady,
		Status: corev1.ConditionUnknown,
	}

	SuccessStatus = shipper.InstallationTargetStatus{
		Conditions: []shipper.TargetCondition{
			TargetConditionOperational,
			TargetConditionReady,
		},
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

func buildChart(appName, version string) shipper.Chart {
	return shipper.Chart{
		Name:    appName,
		Version: version,
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
