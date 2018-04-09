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
	app := newApplication(testAppName)
	rel := newRelease("test-release", app)

	appHash := hashReleaseEnvironment(app.Spec.Template)
	relHash := hashReleaseEnvironment(rel.Environment)
	if appHash != relHash {
		t.Errorf("two identical environments should have hashed to the same value, but they did not: app %q and rel %q", appHash, relHash)
	}

	distinctApp := newApplication(testAppName)
	distinctApp.Spec.Template.Strategy.Name = "blorg"
	distinctHash := hashReleaseEnvironment(distinctApp.Spec.Template)
	if distinctHash == appHash {
		t.Errorf("two different environments hashed to the same thing: %q", distinctHash)
	}
}

// an app with no history should create a release
func TestCreateFirstRelease(t *testing.T) {
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

	f.expectApplicationUpdate(initialApp)
	f.expectReleaseCreate(expectedRelease)
	f.expectApplicationUpdate(finalApp)
	f.run()
}

// an app with 1 existing release should create a new one when its template has changed
func TestCreateSecondRelease(t *testing.T) {
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
	f.objects = append(f.objects, app)

	oldEnvHash := hashReleaseEnvironment(app.Spec.Template)
	oldRelName := fmt.Sprintf("%s-%s-0", testAppName, oldEnvHash)

	release := newRelease(oldRelName, app)
	release.Environment.Chart.RepoURL = srv.URL()
	f.objects = append(f.objects, release)

	app.Status.History = []*shipperv1.ReleaseRecord{
		{
			Name:   oldRelName,
			Status: shipperv1.ReleaseRecordObjectCreated,
		},
	}

	app.Spec.Template.Strategy.Name = "purple-orange"

	newEnvHash := hashReleaseEnvironment(app.Spec.Template)
	newRelName := fmt.Sprintf("%s-%s-0", testAppName, newEnvHash)

	expectedRelease := newRelease(newRelName, app)
	expectedRelease.Status.Phase = shipperv1.ReleasePhaseWaitingForScheduling
	expectedRelease.Labels[shipperv1.ReleaseEnvironmentHashLabel] = newEnvHash
	expectedRelease.Annotations[shipperv1.ReleaseTemplateGenerationAnnotation] = "0"

	finalApp := app.DeepCopy()
	finalApp.Status.History = []*shipperv1.ReleaseRecord{
		{
			Name:   oldRelName,
			Status: shipperv1.ReleaseRecordObjectCreated,
		},
		{
			Name:   newRelName,
			Status: shipperv1.ReleaseRecordObjectCreated,
		},
	}

	// same silly issue as TestCreateFirstRelease
	f.expectApplicationUpdate(finalApp)
	f.expectReleaseCreate(expectedRelease)
	f.expectApplicationUpdate(finalApp)
	f.run()
}

// abort through invalid history entries
func TestCleanBogusHistory(t *testing.T) {
	f := newFixture(t)
	app := newApplication(testAppName)
	f.objects = append(f.objects, app)

	envHash := hashReleaseEnvironment(app.Spec.Template)
	relName := fmt.Sprintf("%s-%s-0", testAppName, envHash)

	release := newRelease(relName, app)
	f.objects = append(f.objects, release)

	finalApp := app.DeepCopy()
	finalApp.Status.History = []*shipperv1.ReleaseRecord{
		{
			Name:   relName,
			Status: shipperv1.ReleaseRecordObjectCreated,
		},
	}

	// this should be reverted to the strategy in the only existing release, which is "foobar"
	app.Spec.Template.Strategy.Name = "some_crazy_strategy"

	// these nonsense history entries should be deleted
	app.Status.History = []*shipperv1.ReleaseRecord{
		{
			Name:   relName,
			Status: shipperv1.ReleaseRecordObjectCreated,
		},
		{
			Name:   "bogus",
			Status: shipperv1.ReleaseRecordObjectCreated,
		},
		{
			Name:   "bits",
			Status: shipperv1.ReleaseRecordObjectCreated,
		},
		{
			Name:   "bamboozle",
			Status: shipperv1.ReleaseRecordObjectCreated,
		},
		{
			Name:   "blindingly",
			Status: shipperv1.ReleaseRecordObjectCreated,
		},
	}

	f.expectApplicationUpdate(finalApp)
	f.run()
}

func newRelease(releaseName string, app *shipperv1.Application) *shipperv1.Release {
	return &shipperv1.Release{
		ReleaseMeta: shipperv1.ReleaseMeta{
			ObjectMeta: metav1.ObjectMeta{
				Name:      releaseName,
				Namespace: app.GetNamespace(),
				Annotations: map[string]string{
					shipperv1.ReleaseReplicasAnnotation: "12", // value from the test chart
				},
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
			Environment: *(app.Spec.Template.DeepCopy()),
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

	c.processNextWorkItem()

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
