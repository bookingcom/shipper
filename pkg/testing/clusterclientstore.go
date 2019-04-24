package testing

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
)

func ClusterClientStore(
	stopCh chan struct{},
	clusterNames []string,
	clientBuilderFunc clusterclientstore.ClientBuilderFunc,
) *clusterclientstore.Store {

	// These are runtime.Object because NewSimpleClientset takes a varargs list of
	// runtime.Object, and Go can't understand structs which fulfill that interface
	// when the function is variadic.
	clusters := []runtime.Object{}
	secrets := []runtime.Object{}

	for i, clusterName := range clusterNames {
		clusters = append(clusters,
			buildCluster(clusterName, fmt.Sprintf("localhost:8%03d", i)),
		)
		secrets = append(secrets,
			buildClusterSecret(clusterName),
		)
	}

	kubeClient := kubefake.NewSimpleClientset(secrets...)
	shipperClient := shipperfake.NewSimpleClientset(clusters...)

	kubeInformerFactory := informers.NewSharedInformerFactory(kubeClient, NoResyncPeriod)
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(shipperClient, NoResyncPeriod)

	store := clusterclientstore.NewStore(
		clientBuilderFunc,
		kubeInformerFactory.Core().V1().Secrets(),
		shipperInformerFactory.Shipper().V1alpha1().Clusters(),
		TestNamespace,
		nil,
		nil,
	)

	kubeInformerFactory.Start(stopCh)
	shipperInformerFactory.Start(stopCh)

	return store
}

func CheckClusterClientActions(
	store *clusterclientstore.Store,
	clusters []*ClusterFixture,
	ua string,
	t *testing.T,
) {
	for _, cluster := range clusters {
		expected := cluster.ExpectedActions()
		client, err := store.GetClient(cluster.Name, ua)
		if err != nil {
			t.Fatalf("error getting cluster client for cluster fixture %q: %q", cluster.Name, err)
			return
		}

		fakeClient := client.(*kubefake.Clientset)
		actual := FilterActions(fakeClient.Actions())
		CheckActions(expected, actual, t)
	}
}

func buildCluster(name, hostname string) *shipper.Cluster {
	return &shipper.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: shipper.ClusterSpec{
			Capabilities: []string{},
			Region:       TestRegion,
			APIMaster:    hostname,
			Scheduler: shipper.ClusterSchedulerSettings{
				Unschedulable: false,
			},
		},
	}
}

func buildClusterSecret(name string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: TestNamespace,
			Annotations: map[string]string{
				shipper.SecretChecksumAnnotation: "checksum",
			},
		},
		Data: map[string][]byte{
			"token":  []byte("fake_bearer_token"),
			"ca.crt": []byte("fake_ca_crt"),
		},
	}
}
