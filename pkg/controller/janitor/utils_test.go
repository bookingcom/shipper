package janitor

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func buildRelease(
	namespace, app, name string, clusters []string,
) *shipper.Release {
	clustersStr := strings.Join(clusters, ",")
	return &shipper.Release{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      fmt.Sprintf("%s-%s", app, name),
			Annotations: map[string]string{
				shipper.ReleaseClustersAnnotation: clustersStr,
			},
			Labels: map[string]string{
				shipper.AppLabel: app,
			},
		},
	}
}

func buildCluster(name string) *shipper.Cluster {
	return &shipper.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}
