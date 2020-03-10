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
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperclient "github.com/bookingcom/shipper/pkg/client"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	"github.com/bookingcom/shipper/pkg/tls"
)

const (
	testClusterName = "minikube"
	testClusterHost = "localhost"
)

var tlsPair = tls.Pair{
	CrtPath: "../tls/testdata/tls.crt",
	KeyPath: "../tls/testdata/tls.key",
}

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
			ua := "foo"
			expected, err := s.GetClient(testClusterName, ua)
			if err != nil {
				t.Errorf("unexpected error getting client %v", err)
			}
			if s.cache.Count() != 1 {
				t.Errorf("expected exactly one cluster, found %q", s.cache.Count())
			}

			found, err := s.GetClient(testClusterName, ua)
			if err != nil {
				t.Errorf("unexpected error getting client %v", err)
			}
			if found != expected {
				t.Errorf("expected client %v to be reused, but instead got a new client %v", expected, found)
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
				_, err := s.GetClient(name, "foo")
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
			_, err := s.GetClient("foo", "baz")
			if !shippererrors.IsClusterNotInStoreError(err) {
				t.Errorf("expected 'no such cluster' error, but got something else: %v", err)
			}
			_, err = s.GetClient("bar", "baz")
			if !shippererrors.IsClusterNotInStoreError(err) {
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
	// This secret key is invalid and therefore the cache won't preserve the
	// cluster
	f.addSecret(newSecret(testClusterName, []byte("crt"), []byte("key")))

	store := f.run()

	wait.PollUntil(
		10*time.Millisecond,
		func() (bool, error) { return true, nil },
		stopAfter(3*time.Second),
	)

	expErr := fmt.Errorf(
		"cannot create client for cluster %q: tls: failed to find any PEM data in certificate input",
		testClusterName)
	err := store.syncCluster(testClusterName)
	if !errEqual(err, expErr) {
		t.Fatalf("unexpected error returned by `store.SyncCluster/0`: got: %s, want: %s",
			err, expErr)
	}
}

func TestReCacheClusterOnSecretUpdate(t *testing.T) {
	f := newFixture(t)

	f.addCluster(testClusterName)

	f.addSecret(newSecret(testClusterName, []byte("crt"), []byte("key")))

	store := f.run()

	wait.PollUntil(
		10*time.Millisecond,
		func() (bool, error) { return true, nil },
		stopAfter(3*time.Second),
	)

	_, ok := store.cache.Fetch(testClusterName)
	if ok {
		t.Fatalf("did not expect to fetch a cluster from the cache")
	}

	secret, err := store.secretInformer.Lister().Secrets(store.ns).Get(testClusterName)
	if err != nil {
		t.Fatalf("failed to fetch secret from lister: %s", err)
	}
	validSecret := newValidSecret(testClusterName)
	secret.Data = validSecret.Data

	client := f.kubeClient
	_, err = client.CoreV1().Secrets(store.ns).Update(secret)
	if err != nil {
		t.Fatalf("failed to update secret: %s", err)
	}

	key := fmt.Sprintf("%s/%s", store.ns, testClusterName)
	if err := store.syncSecret(key); err != nil {
		t.Fatalf("unexpected error returned by `store.syncSecret/1`: %s", err)
	}

	cluster, ok := store.cache.Fetch(testClusterName)
	if !ok {
		t.Fatalf("expected to fetch a cluster from the cache")
	}

	secretChecksum := computeSecretChecksum(secret)
	if clusterChecksum, _ := cluster.GetChecksum(); clusterChecksum != secretChecksum {
		t.Fatalf("inconsistent cluster checksum: got: %s, want: %s",
			clusterChecksum, secretChecksum)
	}
}

func TestConfigTimeout(t *testing.T) {
	f := newFixture(t)

	sevenSeconds := 7 * time.Second
	f.restTimeout = &sevenSeconds

	f.addCluster(testClusterName)
	f.addSecret(newValidSecret(testClusterName))

	store := f.run()

	wait.PollUntil(
		10*time.Millisecond,
		func() (bool, error) {
			cluster, ok := store.cache.Fetch(testClusterName)
			return ok && cluster.IsReady(), nil
		},
		stopAfter(3*time.Second),
	)

	restCfg, err := store.GetConfig(testClusterName)
	if err != nil {
		t.Fatalf("expected a REST config, but got error: %s", err)
	}

	if restCfg.Timeout != sevenSeconds {
		t.Errorf("expected REST config to have timeout of %s, but got %s", sevenSeconds, restCfg.Timeout)
	}
}

type fixture struct {
	t              *testing.T
	s              *Store
	kubeClient     *kubefake.Clientset
	shipperClient  *shipperfake.Clientset
	kubeObjects    []runtime.Object
	shipperObjects []runtime.Object
	restTimeout    *time.Duration
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

	go s.Run(stopCh)
	return s
}

func (f *fixture) newStore() (*Store, kubeinformers.SharedInformerFactory, shipperinformers.SharedInformerFactory) {
	f.kubeClient = kubefake.NewSimpleClientset(f.kubeObjects...)
	f.shipperClient = shipperfake.NewSimpleClientset(f.shipperObjects...)

	noResyncPeriod := time.Duration(0)
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(f.shipperClient, noResyncPeriod)
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(f.kubeClient, noResyncPeriod)

	store := NewStore(
		func(_ string, _ string, config *rest.Config) (kubernetes.Interface, error) {
			return kubernetes.NewForConfig(config)
		},
		func(_, userAgent string, config *rest.Config) (shipperclientset.Interface, error) {
			return shipperclient.NewShipperClient(userAgent, config)
		},
		kubeInformerFactory.Core().V1().Secrets(),
		shipperInformerFactory,
		shipper.ShipperNamespace,
		f.restTimeout,
	)

	return store, kubeInformerFactory, shipperInformerFactory
}

func (f *fixture) addSecret(secret *corev1.Secret) {
	f.kubeObjects = append(f.kubeObjects, secret)
}

func (f *fixture) addCluster(name string) {
	cluster := &shipper.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: shipper.ClusterSpec{
			Capabilities: []string{},
			Region:       "eu",
			APIMaster:    testClusterHost,
		},
	}

	f.shipperObjects = append(f.shipperObjects, cluster)
}

func newValidSecret(name string) *corev1.Secret {
	crt, key, _, err := tlsPair.GetAll()
	if err != nil {
		panic(fmt.Sprintf("could not read test TLS data from paths: %v: %v", tlsPair, err))
	}
	return newSecret(name, crt, key)
}

func newSecret(name string, crt, key []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: shipper.ShipperNamespace,
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

func errEqual(e1, e2 error) bool {
	if e1 == nil || e2 == nil {
		return e1 == e2
	}
	return e1.Error() == e2.Error()
}
