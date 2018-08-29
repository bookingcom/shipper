package clustersecret

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/satori/go.uuid"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	"github.com/bookingcom/shipper/pkg/tls"
)

var tlsPair = tls.Pair{"../../tls/testdata/tls.crt", "../../tls/testdata/tls.key"}

func TestNewCluster(t *testing.T) {
	f := newFixture(t)

	cluster := newCluster("test-cluster")
	f.shipperObjects = append(f.kubeObjects, cluster)

	secret := newClusterSecret(cluster, tlsPair)
	f.expectSecretCreate(secret)

	f.run()
}

func TestUpdateStaleSecret(t *testing.T) {
	f := newFixture(t)

	cluster := newCluster("test-cluster")
	f.shipperObjects = append(f.shipperObjects, cluster)

	newSecret := newClusterSecret(cluster, tlsPair)
	f.expectSecretUpdate(newSecret)

	oldSecret := newSecret.DeepCopy()
	// simulate stale data
	oldSecret.Annotations[shipperv1.SecretChecksumAnnotation] = "stale"
	oldSecret.Data[corev1.TLSCertKey] = []byte{'s', 't', 'a', 'l', 'e'}
	oldSecret.Data[corev1.TLSPrivateKeyKey] = []byte{'s', 't', 'a', 'l', 'e'}
	f.kubeObjects = append(f.kubeObjects, oldSecret)

	f.run()
}

func TestMissingChecksum(t *testing.T) {
	f := newFixture(t)

	cluster := newCluster("test-cluster")
	f.shipperObjects = append(f.shipperObjects, cluster)

	newSecret := newClusterSecret(cluster, tlsPair)
	f.expectSecretUpdate(newSecret)

	oldSecret := newSecret.DeepCopy()
	delete(oldSecret.Annotations, shipperv1.SecretChecksumAnnotation) // Simulate missing annotation.
	f.kubeObjects = append(f.kubeObjects, oldSecret)

	f.run()
}

func TestNoAnnotations(t *testing.T) {
	f := newFixture(t)

	cluster := newCluster("test-cluster")
	f.shipperObjects = append(f.shipperObjects, cluster)

	newSecret := newClusterSecret(cluster, tlsPair)
	f.expectSecretUpdate(newSecret)

	oldSecret := newSecret.DeepCopy()
	oldSecret.Annotations = nil // Simulate missing annotation.
	f.kubeObjects = append(f.kubeObjects, oldSecret)

	f.run()
}

func TestSecretNotControlledByUs(t *testing.T) {
	f := newFixture(t)

	cluster := newCluster("test-cluster")
	f.shipperObjects = append(f.shipperObjects, cluster)

	clusterSecret := newClusterSecret(cluster, tlsPair)
	genericTLSSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-secret",
			Namespace: shippertesting.TestNamespace,
		},
		Data: map[string][]byte{
			corev1.TLSCertKey:       []byte{'f', 'o', 'o'},
			corev1.TLSPrivateKeyKey: []byte{'b', 'a', 'r'},
		},
		Type: corev1.SecretTypeTLS,
	}
	saSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-token-8pwp2",
			Namespace: shippertesting.TestNamespace,
			Annotations: map[string]string{
				"kubernetes.io/service-account.name": "default",
				"kubernetes.io/service-account.uid:": "ce549821-0cc2-11e8-a251-080027b50349",
			},
		},
		Data: map[string][]byte{
			"ca.crt":    []byte{'c', 'a', '.', 'c', 'r', 't'},
			"namespace": []byte{'n', 'a', 'm', 'e', 's', 'p', 'a', 'c', 'e'},
			"token":     []byte{'t', 'o', 'k', 'e', 'n'},
		},
		Type: corev1.SecretTypeServiceAccountToken,
	}
	f.kubeObjects = append(f.kubeObjects, clusterSecret, genericTLSSecret, saSecret)

	// not expecting any changes

	f.run()
}

func newCluster(name string) *shipperv1.Cluster {
	return &shipperv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  types.UID(uuid.NewV4().String()),
		},
		Spec: shipperv1.ClusterSpec{
			Capabilities: []string{"gpu", "pci"},
			Region:       "eu",
			APIMaster:    "https://192.168.1.100:8443",
		},
	}
}

func newClusterSecret(cluster *shipperv1.Cluster, p tls.Pair) *corev1.Secret {
	crt, key, csum, _ := p.GetAll()

	name := cluster.GetName()

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: shippertesting.TestNamespace,
			Annotations: map[string]string{
				shipperv1.SecretClusterNameAnnotation: name,
				shipperv1.SecretChecksumAnnotation:    hex.EncodeToString(csum),
			},
			OwnerReferences: []metav1.OwnerReference{
				metav1.OwnerReference{
					APIVersion: "shipper.booking.com/v1",
					Kind:       "Cluster",
					Name:       name,
					UID:        cluster.GetUID(),
				},
			},
		},
		Data: map[string][]byte{
			corev1.TLSCertKey:       crt,
			corev1.TLSPrivateKeyKey: key,
		},
		Type: corev1.SecretTypeTLS,
	}
}

type fixture struct {
	t                *testing.T
	kubeClientset    *fake.Clientset
	kubeObjects      []runtime.Object
	shipperClientset *shipperfake.Clientset
	shipperObjects   []runtime.Object
	actions          []kubetesting.Action
}

func newFixture(t *testing.T) *fixture {
	return &fixture{
		t: t,
	}
}

func (f *fixture) newController() (*Controller, shipperinformers.SharedInformerFactory, informers.SharedInformerFactory) {
	f.shipperClientset = shipperfake.NewSimpleClientset(f.shipperObjects...)
	f.kubeClientset = fake.NewSimpleClientset(f.kubeObjects...)

	const noResyncPeriod time.Duration = 0
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(f.shipperClientset, noResyncPeriod)
	kubeInformerFactory := informers.NewSharedInformerFactory(f.kubeClientset, noResyncPeriod)

	c := NewController(
		shipperInformerFactory,
		f.kubeClientset,
		kubeInformerFactory,
		tlsPair.CrtPath,
		tlsPair.KeyPath,
		shippertesting.TestNamespace,
		record.NewFakeRecorder(42),
	)

	return c, shipperInformerFactory, kubeInformerFactory
}

func (f *fixture) run() {
	c, si, ki := f.newController()

	stopCh := make(chan struct{})
	defer close(stopCh)

	si.Start(stopCh)
	ki.Start(stopCh)

	si.WaitForCacheSync(stopCh)
	ki.WaitForCacheSync(stopCh)

	wait.PollUntil(
		10*time.Millisecond,
		func() (bool, error) { return c.workqueue.Len() >= 1, nil },
		stopCh,
	)

	c.processNextWorkItem()

	actual := shippertesting.FilterActions(f.kubeClientset.Actions())
	shippertesting.CheckActions(f.actions, actual, f.t)
}

func (f *fixture) expectSecretUpdate(secret *corev1.Secret) {
	gvr := corev1.SchemeGroupVersion.WithResource("secrets")
	action := kubetesting.NewUpdateAction(gvr, secret.GetNamespace(), secret)

	f.actions = append(f.actions, action)
}

func (f *fixture) expectSecretCreate(secret *corev1.Secret) {
	gvr := corev1.SchemeGroupVersion.WithResource("secrets")
	action := kubetesting.NewCreateAction(gvr, secret.GetNamespace(), secret)

	f.actions = append(f.actions, action)

}

// NOTE(asurikov): test garbage collection?
