package application

import (
	"fmt"
	"os"
	"testing"
	"time"

	//corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	//runtimeutil "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"k8s.io/helm/pkg/repo/repotest"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"github.com/bookingcom/shipper/pkg/chart"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

const (
	testAppName = "test-app"
)

// private method, but other tests make use of it
func TestHashReleaseEnv(t *testing.T) {

}

// an app with no history should create a release
func TestCreateRelease(t *testing.T) {
	srv, hh, err := repotest.NewTempServer("testdata/*.tgz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		os.RemoveAll(hh.String())
		srv.Stop()
	}()

	f := newFixture(t)
	app := newApplication(testAppName)
	app.Spec.Template.Chart.RepoURL = srv.URL()

	envHash := hashReleaseEnvironment(app.Spec.Template)
	expectedRelName := fmt.Sprintf("%s-%s-0", testAppName, envHash)

	f.objects = append(f.objects, app)
	initialApp := app.DeepCopy()
	initialApp.Status.History = []*shipperv1.ReleaseRecord{
		{
			Name: expectedRelName,
			// NOTE(btyler) This is wrong, but the reason is interesting. This
			// should be ReleaseRecordWaitingForObject. However, kubetesting
			// does not DeepCopy the objects associated with Update calls, and
			// so when we later mutate the application object to change the
			// release history record, we mutate it in the kubetesting Action
			// object reference as well. We _could_ fix this by sprinkling
			// app = app.DeepCopy around our code, but then we'd be adding
			// silliness to production code in order to satisfy a testing
			// library. So, we put a knowingly-false thing into the tests.
			Status: shipperv1.ReleaseRecordObjectCreated,
		},
	}

	finalApp := app.DeepCopy()
	finalApp.Status.History = []*shipperv1.ReleaseRecord{
		{
			Name:   expectedRelName,
			Status: shipperv1.ReleaseRecordObjectCreated,
		},
	}

	expectedRelease := newRelease(expectedRelName, app)
	expectedRelease.Environment.Chart.RepoURL = srv.URL()
	expectedRelease.Status.Phase = shipperv1.ReleasePhaseWaitingForScheduling
	expectedRelease.Labels[shipperv1.ReleaseEnvironmentHashLabel] = envHash
	expectedRelease.Annotations[shipperv1.ReleaseTemplateGenerationAnnotation] = "0"
	expectedRelease.Annotations[shipperv1.ReleaseReplicasAnnotation] = fmt.Sprintf("%d", 12)

	f.expectApplicationUpdate(initialApp)
	f.expectReleaseCreate(expectedRelease)
	f.expectApplicationUpdate(finalApp)
	f.run()
}

func newRelease(releaseName string, app *shipperv1.Application) *shipperv1.Release {
	return &shipperv1.Release{
		ReleaseMeta: shipperv1.ReleaseMeta{
			ObjectMeta: metav1.ObjectMeta{
				Name:        releaseName,
				Namespace:   app.GetNamespace(),
				Annotations: map[string]string{},
				Labels: map[string]string{
					shipperv1.ReleaseLabel: releaseName,
					shipperv1.AppLabel:     app.GetName(),
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "shipper.booking.com/v1",
						Kind:       "Application",
						Name:       app.GetName(),
					},
				},
			},
			Environment: shipperv1.ReleaseEnvironment{
				Chart: shipperv1.Chart{
					Name:    "simple",
					Version: "0.0.1",
					RepoURL: "http://127.0.0.1:8879/charts",
				},
				Strategy:         shipperv1.ReleaseStrategy{Name: "foobar"},
				ClusterSelectors: []shipperv1.ClusterSelector{},
				Values:           &shipperv1.ChartValues{},
				Replicas:         int32(21),
			},
		},
	}
}

func newApplication(name string) *shipperv1.Application {
	return &shipperv1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: shippertesting.TestNamespace,
		},
		Spec: shipperv1.ApplicationSpec{
			Template: shipperv1.ReleaseEnvironment{
				Chart: shipperv1.Chart{
					Name:    "simple",
					Version: "0.0.1",
					RepoURL: "http://127.0.0.1:8879/charts",
				},
				Strategy:         shipperv1.ReleaseStrategy{Name: "foobar"},
				ClusterSelectors: []shipperv1.ClusterSelector{},
				Values:           &shipperv1.ChartValues{},
				Replicas:         int32(21),
			},
		},
	}
}

type fixture struct {
	t       *testing.T
	client  *shipperfake.Clientset
	actions []kubetesting.Action
	objects []runtime.Object
}

func newFixture(t *testing.T) *fixture {
	return &fixture{t: t}
}

func (f *fixture) newController() (*Controller, shipperinformers.SharedInformerFactory) {
	f.client = shipperfake.NewSimpleClientset(f.objects...)

	const noResyncPeriod time.Duration = 0
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(f.client, noResyncPeriod)

	c := NewController(f.client, shipperInformerFactory, record.NewFakeRecorder(42), chart.FetchRemote())

	return c, shipperInformerFactory
}

func (f *fixture) run() {
	c, i := f.newController()

	stopCh := make(chan struct{})
	defer close(stopCh)

	i.Start(stopCh)
	i.WaitForCacheSync(stopCh)

	wait.PollUntil(
		10*time.Millisecond,
		func() (bool, error) { return c.appWorkqueue.Len() >= 1, nil },
		stopCh,
	)

	processNextWorkItem(c.appWorkqueue, c.syncApplication)

	actual := shippertesting.FilterActions(f.client.Actions())
	shippertesting.CheckActions(f.actions, actual, f.t)
}

func (f *fixture) expectReleaseCreate(rel *shipperv1.Release) {
	gvr := shipperv1.SchemeGroupVersion.WithResource("releases")
	action := kubetesting.NewCreateAction(gvr, rel.GetNamespace(), rel)

	f.actions = append(f.actions, action)
}

func (f *fixture) expectApplicationUpdate(app *shipperv1.Application) {
	gvr := shipperv1.SchemeGroupVersion.WithResource("applications")
	action := kubetesting.NewUpdateAction(gvr, app.GetNamespace(), app)

	f.actions = append(f.actions, action)
}
