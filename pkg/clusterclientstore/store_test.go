package clusterclientstore

import (
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/clusterclientstore/cache"
	"github.com/bookingcom/shipper/pkg/tls"
)

const (
	testClusterName = "minikube"
	testClusterHost = "localhost"
)

var tlsPair = tls.Pair{"../tls/testdata/tls.crt", "../tls/testdata/tls.key"}

type clusters []string
type secrets []string

func TestClientCreation(t *testing.T) {
	clientStoreTestCase(t, "creates config",
		clusters{testClusterName},
		secrets{testClusterName},
		func(s *Store) (bool, error) {
			cluster, ok := s.cache.Fetch(testClusterName)
			return ok && cluster.IsReady(), nil
		},
		func(s *Store) {
			config, err := s.GetConfig(testClusterName)
			if err != nil {
				t.Errorf("unexpected error getting config %v", err)
			}
			if config.Host != testClusterHost {
				t.Errorf("expected config with host %q but got %q", testClusterHost, config.Host)
			}

			if s.cache.Count() != 1 {
				t.Errorf("expected exactly one cluster, found %q", s.cache.Count())
			}
		})

	clientStoreTestCase(t, "creates client",
		clusters{testClusterName},
		secrets{testClusterName},
		func(s *Store) (bool, error) {
			cluster, ok := s.cache.Fetch(testClusterName)
			return ok && cluster.IsReady(), nil
		},
		func(s *Store) {
			_, err := s.GetClient(testClusterName)
			if err != nil {
				t.Errorf("unexpected error getting client %v", err)
			}
			if s.cache.Count() != 1 {
				t.Errorf("expected exactly one cluster, found %q", s.cache.Count())
			}
		})

	clientStoreTestCase(t, "creates informerFactory",
		clusters{testClusterName},
		secrets{testClusterName},
		func(s *Store) (bool, error) {
			cluster, ok := s.cache.Fetch(testClusterName)
			return ok && cluster.IsReady(), nil
		},
		func(s *Store) {
			_, err := s.GetInformerFactory(testClusterName)
			if err != nil {
				t.Errorf("unexpected error getting informerFactory %v", err)
			}
			if s.cache.Count() != 1 {
				t.Errorf("expected exactly one cluster, found %q", s.cache.Count())
			}
		})

	clusterList := []string{"foo", "bar", "baz", "qux", "warble"}
	clientStoreTestCase(t, "creates multiple clusters",
		clusters(clusterList),
		secrets(clusterList),
		func(s *Store) (bool, error) {
			ready := true
			for _, name := range clusterList {
				cluster, ok := s.cache.Fetch(name)
				ready = ready && ok && cluster.IsReady()
			}
			return ready, nil
		},
		func(s *Store) {
			for _, name := range clusterList {
				_, err := s.GetClient(name)
				if err != nil {
					t.Errorf("unexpected error getting client %q %v", name, err)
				}

				config, err := s.GetConfig(name)
				if err != nil {
					t.Errorf("unexpected error getting config for %q %v", name, err)
				}
				if config.Host != testClusterHost {
					t.Errorf("expected config with host %q but got %q", testClusterHost, config.Host)
				}

				_, err = s.GetInformerFactory(name)
				if err != nil {
					t.Errorf("unexpected error getting informerFactory %q %v", name, err)
				}
			}

			if s.cache.Count() != len(clusterList) {
				t.Errorf("expected exactly %d clusters, found %d", len(clusterList), s.cache.Count())
			}
		})

}

func TestNoClientGeneration(t *testing.T) {
	clientStoreTestCase(t, "mismatch results in no client (for either name)",
		clusters{"foo"},
		secrets{"bar"},
		func(s *Store) (bool, error) {
			// no waiting: we don't expect any clients
			// we expect "no such cluster" because that means this cluster is entirely missing
			return true, nil
		},
		func(s *Store) {
			_, err := s.GetClient("foo")
			if err != cache.ErrClusterNotInStore {
				t.Errorf("expected 'no such cluster' error, but got something else: %v", err)
			}
			_, err = s.GetClient("bar")
			if err != cache.ErrClusterNotInStore {
				t.Errorf("expected 'no such cluster' error, but got something else: %v", err)
			}

			if s.cache.Count() > 0 {
				t.Errorf("expected zero populated clusters, but found %q", s.cache.Count())
			}
		})
}

func clientStoreTestCase(
	t *testing.T, name string,
	clusters, secrets []string,
	waitCondition func(*Store) (bool, error), ready func(*Store),
) {
	f := newFixture(t)
	for _, clusterName := range clusters {
		f.addCluster(clusterName)
	}

	for _, secretName := range secrets {
		f.addSecret(newValidSecret(secretName))
	}

	store := f.run()

	wait.PollUntil(
		10*time.Millisecond,
		func() (bool, error) { return waitCondition(store) },
		stopAfter(3*time.Second),
	)

	ready(store)
}

func TestInvalidClientCredentials(t *testing.T) {
	f := newFixture(t)

	f.addCluster(testClusterName)
	f.addSecret(newSecret(testClusterName, []byte("crt"), []byte("key"), []byte("checksum")))

	store := f.run()

	wait.PollUntil(
		10*time.Millisecond,
		func() (bool, error) { return true, nil },
		stopAfter(3*time.Second),
	)

	_, err := store.GetConfig(testClusterName)
	if err != cache.ErrClusterNotInStore {
		t.Errorf("expected NoSuchCluster for cluster called %q for invalid client credentials; instead got %v", testClusterName, err)
	}
}

type fixture struct {
	t              *testing.T
	s              *Store
	kubeClient     *kubefake.Clientset
	shipperClient  *shipperfake.Clientset
	kubeObjects    []runtime.Object
	shipperObjects []runtime.Object
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{t: t}
	return f
}

func (f *fixture) run() *Store {
	s, kubeInformerFactory, shipperInformerFactory := f.newStore()
	f.s = s
	stopCh := make(chan struct{})

	go kubeInformerFactory.Start(stopCh)
	go shipperInformerFactory.Start(stopCh)
	kubeInformerFactory.WaitForCacheSync(stopCh)
	shipperInformerFactory.WaitForCacheSync(stopCh)

	s.Run(stopCh)
	return s
}

func (f *fixture) newStore() (*Store, kubeinformers.SharedInformerFactory, shipperinformers.SharedInformerFactory) {
	f.kubeClient = kubefake.NewSimpleClientset(f.kubeObjects...)
	f.shipperClient = shipperfake.NewSimpleClientset(f.shipperObjects...)

	const noResyncPeriod time.Duration = 0
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(f.shipperClient, noResyncPeriod)
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(f.kubeClient, noResyncPeriod)

	store := NewStore(
		kubeInformerFactory.Core().V1().Secrets(),
		shipperInformerFactory.Shipper().V1().Clusters(),
	)

	return store, kubeInformerFactory, shipperInformerFactory
}

func (f *fixture) addSecret(secret *corev1.Secret) {
	f.kubeObjects = append(f.kubeObjects, secret)
}

func (f *fixture) addCluster(name string) {
	cluster := &shipperv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: shipperv1.ClusterSpec{
			Capabilities: []string{},
			Region:       "eu",
			APIMaster:    testClusterHost,
		},
	}

	f.shipperObjects = append(f.shipperObjects, cluster)
}

func newValidSecret(name string) *corev1.Secret {
	crt, key, checksum, err := tlsPair.GetAll()
	if err != nil {
		panic(fmt.Sprintf("could not read test TLS data from paths: %v: %v", tlsPair, err))
	}
	return newSecret(name, crt, key, checksum)
}

func newSecret(name string, crt, key, checksum []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: shipperv1.ShipperNamespace,
			Annotations: map[string]string{
				shipperv1.SecretClusterNameAnnotation: name,
				shipperv1.SecretChecksumAnnotation:    string(checksum),
			},
		},
		Data: map[string][]byte{
			corev1.TLSCertKey:       crt,
			corev1.TLSPrivateKeyKey: key,
		},
		Type: corev1.SecretTypeTLS,
	}
}

func stopAfter(t time.Duration) <-chan struct{} {
	stopCh := make(chan struct{})
	go func() {
		<-time.After(t)
		close(stopCh)
	}()
	return stopCh
}
