package release

import (
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	//"k8s.io/apimachinery/pkg/util/wait"
	fakediscovery "k8s.io/client-go/discovery/fake"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"k8s.io/helm/pkg/repo/repotest"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/chart"
	//shipperclient "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/conditions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

const (
	clusterName   = "minikube"
	namespace     = "test-namespace"
	incumbentName = "0.0.1"
	contenderName = "0.0.2"
)

var chartRepoURL string

func init() {
	releaseutil.ConditionsShouldDiscardTimestamps = true
	conditions.StrategyConditionsShouldDiscardTimestamps = true
}

var app *shipper.Application

type context struct {
	t          *testing.T
	namespace  string
	controller *ReleaseController

	app          *shipper.Application
	chart        *shipper.Chart
	contender    *releaseInfo
	incumbent    *releaseInfo
	chartRepoURL string

	clientset       *shipperfake.Clientset
	informerFactory informers.SharedInformerFactory
	discovery       *fakediscovery.FakeDiscovery
	clientPool      *fakedynamic.FakeClientPool
	actions         []kubetesting.Action
	objects         []runtime.Object
	clusters        []string
	receivedEvents  []string
	expectedEvents  []string
	recorder        *record.FakeRecorder
}

func newContext(t *testing.T) *context {
	return &context{
		t:              t,
		actions:        make([]kubetesting.Action, 0),
		objects:        make([]runtime.Object, 0),
		clusters:       make([]string, 0),
		receivedEvents: make([]string, 0),
		expectedEvents: make([]string, 0),
		chartRepoURL:   chartRepoURL,
	}
}

func (ctx *context) withNamespace(namespace string) *context {
	ctx.namespace = namespace
	return ctx
}

func (ctx *context) withClusters(clusters ...string) *context {
	ctx.clusters = clusters
	return ctx
}

func (ctx *context) buildChart() *context {
	ctx.chart = &shipper.Chart{
		Name:    "simple",
		Version: "0.0.1",
		RepoURL: chartRepoURL,
	}
	return ctx
}

func (ctx *context) buildApp(appName string) *context {
	ctx.app = &shipper.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appName,
			Namespace: ctx.namespace,
			UID:       "foobarbaz",
		},
		Status: shipper.ApplicationStatus{
			History: []string{},
		},
	}
	return ctx
}

func (ctx *context) buildContender(contenderName string, totalReplicaCount uint) *context {
	release := &shipper.Release{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "Release",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      contenderName,
			Namespace: ctx.namespace,
			OwnerReferences: []metav1.OwnerReference{
				metav1.OwnerReference{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Kind:       "Application",
					Name:       ctx.app.GetName(),
					UID:        ctx.app.GetUID(),
				},
			},
			Labels: map[string]string{
				shipper.ReleaseLabel: contenderName,
				shipper.AppLabel:     ctx.app.GetName(),
			},
			Annotations: map[string]string{
				shipper.ReleaseGenerationAnnotation: "1",
			},
		},
		Status: shipper.ReleaseStatus{
			Conditions: []shipper.ReleaseCondition{
				{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
			},
		},
		Spec: shipper.ReleaseSpec{
			TargetStep: 0,
			Environment: shipper.ReleaseEnvironment{
				Strategy: &vanguard,
			},
		},
	}

	installationTargetStatuses := make([]*shipper.ClusterInstallationStatus, 0, len(ctx.clusters))
	for _, clusterName := range ctx.clusters {
		installationTargetStatuses = append(installationTargetStatuses, &shipper.ClusterInstallationStatus{
			Name:   clusterName,
			Status: shipper.InstallationStatusInstalled,
		})
	}

	installationTarget := &shipper.InstallationTarget{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "InstallationTarget",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ctx.namespace,
			Name:      contenderName,
			OwnerReferences: []metav1.OwnerReference{metav1.OwnerReference{
				APIVersion: shipper.SchemeGroupVersion.String(),
				Name:       release.Name,
				Kind:       "Release",
				UID:        release.UID,
			}},
		},
		Status: shipper.InstallationTargetStatus{
			Clusters: installationTargetStatuses,
		},
		Spec: shipper.InstallationTargetSpec{
			Clusters: ctx.clusters,
		},
	}

	capacityTargetStatuses := make([]shipper.ClusterCapacityStatus, 0, len(ctx.clusters))
	for _, clusterName := range ctx.clusters {
		capacityTargetStatuses = append(capacityTargetStatuses, shipper.ClusterCapacityStatus{
			Name:            clusterName,
			AchievedPercent: 100,
		})
	}

	capacityTargetSpecs := make([]shipper.ClusterCapacityTarget, 0, len(ctx.clusters))
	for _, clusterName := range ctx.clusters {
		capacityTargetSpecs = append(capacityTargetSpecs, shipper.ClusterCapacityTarget{
			Name:              clusterName,
			Percent:           0,
			TotalReplicaCount: int32(totalReplicaCount),
		})
	}

	capacityTarget := &shipper.CapacityTarget{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "CapacityTarget",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ctx.namespace,
			Name:      contenderName,
			OwnerReferences: []metav1.OwnerReference{metav1.OwnerReference{
				APIVersion: shipper.SchemeGroupVersion.String(),
				Name:       release.Name,
				Kind:       "Release",
				UID:        release.UID,
			}},
		},
		Status: shipper.CapacityTargetStatus{
			Clusters: capacityTargetStatuses,
		},
		Spec: shipper.CapacityTargetSpec{
			Clusters: capacityTargetSpecs,
		},
	}

	trafficTargetStatuses := make([]*shipper.ClusterTrafficStatus, 0, len(ctx.clusters))
	for _, clusterName := range ctx.clusters {
		trafficTargetStatuses = append(trafficTargetStatuses, &shipper.ClusterTrafficStatus{
			Name:            clusterName,
			AchievedTraffic: 100,
		})
	}

	trafficTargetSpecs := make([]shipper.ClusterTrafficTarget, 0, len(ctx.clusters))
	for _, clusterName := range ctx.clusters {
		trafficTargetSpecs = append(trafficTargetSpecs, shipper.ClusterTrafficTarget{
			Name:   clusterName,
			Weight: 0,
		})
	}

	trafficTarget := &shipper.TrafficTarget{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "TrafficTarget",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ctx.namespace,
			Name:      contenderName,
			OwnerReferences: []metav1.OwnerReference{metav1.OwnerReference{
				APIVersion: shipper.SchemeGroupVersion.String(),
				Name:       release.Name,
				Kind:       "Release",
				UID:        release.UID,
			}},
		},
		Status: shipper.TrafficTargetStatus{
			Clusters: trafficTargetStatuses,
		},
		Spec: shipper.TrafficTargetSpec{
			Clusters: trafficTargetSpecs,
		},
	}

	ctx.contender = &releaseInfo{
		release:            release,
		installationTarget: installationTarget,
		capacityTarget:     capacityTarget,
		trafficTarget:      trafficTarget,
	}

	return ctx
}

func (ctx *context) buildIncumbent(incumbentName string, totalReplicaCount uint) *context {

	release := &shipper.Release{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "Release",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      incumbentName,
			Namespace: ctx.namespace,
			OwnerReferences: []metav1.OwnerReference{
				metav1.OwnerReference{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Kind:       "Application",
					Name:       ctx.app.GetName(),
					UID:        ctx.app.GetUID(),
				},
			},
			Labels: map[string]string{
				shipper.ReleaseLabel: incumbentName,
				shipper.AppLabel:     ctx.app.GetName(),
			},
			Annotations: map[string]string{
				shipper.ReleaseGenerationAnnotation: "0",
			},
		},
		Status: shipper.ReleaseStatus{
			AchievedStep: &shipper.AchievedStep{
				Step: 2,
				Name: "full on",
			},
			Conditions: []shipper.ReleaseCondition{
				{Type: shipper.ReleaseConditionTypeInstalled, Status: corev1.ConditionTrue},
				{Type: shipper.ReleaseConditionTypeComplete, Status: corev1.ConditionTrue},
			},
		},
		Spec: shipper.ReleaseSpec{
			TargetStep: 2,
			Environment: shipper.ReleaseEnvironment{
				Strategy: &vanguard,
			},
		},
	}

	clusterInstallationStatuses := make([]*shipper.ClusterInstallationStatus, 0, len(ctx.clusters))
	for _, clusterName := range ctx.clusters {
		clusterInstallationStatuses = append(clusterInstallationStatuses, &shipper.ClusterInstallationStatus{
			Name:   clusterName,
			Status: shipper.InstallationStatusInstalled,
		})
	}

	installationTarget := &shipper.InstallationTarget{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "InstallationTarget",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      incumbentName,
			Namespace: ctx.namespace,
			OwnerReferences: []metav1.OwnerReference{metav1.OwnerReference{
				APIVersion: shipper.SchemeGroupVersion.String(),
				Name:       release.Name,
				Kind:       "Release",
				UID:        release.UID,
			}},
		},
		Status: shipper.InstallationTargetStatus{
			Clusters: clusterInstallationStatuses,
		},
		Spec: shipper.InstallationTargetSpec{
			Clusters: ctx.clusters,
		},
	}

	capacityTargetStatuses := make([]shipper.ClusterCapacityStatus, 0, len(ctx.clusters))
	for _, clusterName := range ctx.clusters {
		capacityTargetStatuses = append(capacityTargetStatuses, shipper.ClusterCapacityStatus{
			Name:            clusterName,
			AchievedPercent: 100,
		})
	}

	capacityTargetSteps := make([]shipper.ClusterCapacityTarget, 0, len(ctx.clusters))
	for _, clusterName := range ctx.clusters {
		capacityTargetSteps = append(capacityTargetSteps, shipper.ClusterCapacityTarget{
			Name:              clusterName,
			Percent:           100,
			TotalReplicaCount: int32(totalReplicaCount),
		})
	}

	capacityTarget := &shipper.CapacityTarget{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "CapacityTarget",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ctx.namespace,
			Name:      incumbentName,
			OwnerReferences: []metav1.OwnerReference{metav1.OwnerReference{
				APIVersion: shipper.SchemeGroupVersion.String(),
				Name:       release.Name,
				Kind:       "Release",
				UID:        release.UID,
			}},
		},
		Status: shipper.CapacityTargetStatus{
			Clusters: capacityTargetStatuses,
		},
		Spec: shipper.CapacityTargetSpec{
			Clusters: capacityTargetSteps,
		},
	}

	clusterTrafficStatuses := make([]*shipper.ClusterTrafficStatus, 0, len(ctx.clusters))
	for _, clusterName := range ctx.clusters {
		clusterTrafficStatuses = append(clusterTrafficStatuses, &shipper.ClusterTrafficStatus{
			Name:            clusterName,
			AchievedTraffic: 100,
		})
	}

	trafficTargetSpecs := make([]shipper.ClusterTrafficTarget, 0, len(ctx.clusters))
	for _, clusterName := range ctx.clusters {
		trafficTargetSpecs = append(trafficTargetSpecs, shipper.ClusterTrafficTarget{
			Name:   clusterName,
			Weight: 100,
		})
	}

	trafficTarget := &shipper.TrafficTarget{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "TrafficTarget",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ctx.namespace,
			Name:      incumbentName,
			OwnerReferences: []metav1.OwnerReference{metav1.OwnerReference{
				APIVersion: shipper.SchemeGroupVersion.String(),
				Name:       release.Name,
				Kind:       "Release",
				UID:        release.UID,
			}},
		},
		Status: shipper.TrafficTargetStatus{
			Clusters: clusterTrafficStatuses,
		},
		Spec: shipper.TrafficTargetSpec{
			Clusters: trafficTargetSpecs,
		},
	}

	ctx.incumbent = &releaseInfo{
		release:            release,
		installationTarget: installationTarget,
		capacityTarget:     capacityTarget,
		trafficTarget:      trafficTarget,
	}

	return ctx
}

func (ctx *context) buildInformerFactory() *context {
	ctx.informerFactory = shipperinformers.NewSharedInformerFactory(ctx.clientset, time.Millisecond*0)
	return ctx
}

func (ctx *context) buildClientset(fixtures []runtime.Object) *context {
	ctx.clientset = shipperfake.NewSimpleClientset(fixtures...)
	return ctx
}

func (ctx *context) buildDiscovery() *context {
	fakeDiscovery, _ := ctx.clientset.Discovery().(*fakediscovery.FakeDiscovery)
	ctx.discovery = fakeDiscovery
	ctx.discovery.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: shipper.SchemeGroupVersion.String(),
			APIResources: []metav1.APIResource{
				{
					Kind:       "Application",
					Namespaced: true,
					Name:       "applications",
				},
				{
					Kind:       "Release",
					Namespaced: true,
					Name:       "releases",
				},
				{
					Kind:       "CapacityTarget",
					Namespaced: true,
					Name:       "capacitytargets",
				},
				{
					Kind:       "InstallationTarget",
					Namespaced: true,
					Name:       "installationtargets",
				},
				{
					Kind:       "TrafficTarget",
					Namespaced: true,
					Name:       "traffictargets",
				},
			},
		},
	}

	return ctx
}

func (ctx *context) buildRecorder() *context {
	ctx.recorder = record.NewFakeRecorder(42)
	return ctx
}

func (ctx *context) buildController() *context {

	if ctx.clientset == nil {
		panic("context clientset is nil")
	}

	if ctx.informerFactory == nil {
		panic("context informer factory is nil")
	}

	if ctx.recorder == nil {
		panic("context recorder is nil")
	}

	controller := NewReleaseController(
		ctx.clientset,
		ctx.informerFactory,
		chart.FetchRemote(),
		&fakedynamic.FakeClientPool{},
		ctx.recorder,
	)

	stopCh := make(chan struct{})
	defer close(stopCh)

	ctx.informerFactory.Start(stopCh)
	ctx.informerFactory.WaitForCacheSync(stopCh)

	ctx.controller = controller

	return ctx
}

func TestMain(m *testing.M) {
	srv, hh, err := repotest.NewTempServer("testdata/*.tgz")
	if err != nil {
		log.Fatal(err)
	}

	chartRepoURL = srv.URL()

	result := m.Run()

	// If the test panics for any reason, the test server doesn't get cleaned up.
	os.RemoveAll(hh.String())
	srv.Stop()

	os.Exit(result)
}

func TestControllerComputeTargetClusters(t *testing.T) {

	ctx := newContext(t).
		withNamespace("test-namespace").
		withClusters("minikube-a").
		buildApp("test-app").
		buildChart().
		buildContender("test-release", 1)

	ctx.contender.release.Annotations[shipper.ReleaseClustersAnnotation] = strings.Join(ctx.clusters, ",")

	expected := ctx.contender.release.DeepCopy()
	expected.Annotations[shipper.ReleaseClustersAnnotation] = strings.Join(ctx.clusters, ",")

	condition := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&expected.Status, *condition)

	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			ctx.namespace,
			expected), // `expected` contains the info about the chosen cluster
	}

	fixtures := make([]runtime.Object, 1, len(ctx.clusters)+1)
	fixtures[0] = ctx.contender.release
	for _, clusterName := range ctx.clusters {
		fixtures = append(fixtures, runtime.Object(buildCluster(clusterName)))
	}

	ctx.buildClientset(fixtures).
		buildInformerFactory().
		buildController()

	ctx.controller.processNextReleaseWorkItem()

	filteredActions := filterActions(ctx.clientset.Actions(), []string{"update"}, []string{"releases"})

	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func TestControllerCreateAssociatedObjects(t *testing.T) {

	ctx := newContext(t).
		withNamespace("test-namespace").
		withClusters("minikube-a").
		buildApp("test-app").
		buildChart().
		buildContender("test-release", 1)

	ctx.contender.release.Annotations[shipper.ReleaseClustersAnnotation] = strings.Join(ctx.clusters, ",")

	fixtures := []runtime.Object{ctx.contender.release}
	for _, clusterName := range ctx.clusters {
		fixtures = append(fixtures, buildCluster(clusterName))
	}

	expected := ctx.contender.release.DeepCopy()
	expected.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}

	installationTarget := buildInstallationTarget(ctx.namespace, expected)
	trafficTarget := buildTrafficTarget(ctx.namespace, expected)
	capacityTarget := buildCapacityTarget(ctx.namespace, expected)

	expectedActions := []kubetesting.Action{
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("installationtargets"),
			ctx.namespace,
			installationTarget),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("traffictargets"),
			ctx.namespace,
			trafficTarget),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("capacitytargets"),
			ctx.namespace,
			capacityTarget,
		),
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			ctx.namespace,
			expected),
	}

	clientset := shipperfake.NewSimpleClientset(fixtures...)

	controller := newReleaseController(clientset)
	controller.processNextReleaseWorkItem()

	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

var interestingReplicaCounts = []uint{1, 3, 10}

func TestContenderReleasePhaseIsWaitingForCommandForInitialStepState(t *testing.T) {

	for _, totalReplicaCount := range interestingReplicaCounts {

		ctx := newContext(t).
			withNamespace("test-namespace").
			withClusters("minikube-a").
			buildInformerFactory().
			buildApp("test-app").
			buildChart().
			buildContender(contenderName, totalReplicaCount).
			buildIncumbent(incumbentName, totalReplicaCount)

		// Strategy specifies that step 0 the contender has a minimum number of pods
		// (1), no traffic yet.

		fmt.Printf("contender capacity targer: %#v\n", ctx.contender.capacityTarget.Spec)

		ctx.contender.capacityTarget.Spec.Clusters[0].Percent = 1
		ctx.contender.capacityTarget.Status.Clusters[0].AvailableReplicas = 1
		ctx.incumbent.capacityTarget.Spec.Clusters[0].Percent = 100
		ctx.incumbent.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(totalReplicaCount)

		fixtures := []runtime.Object{
			ctx.contender.release.DeepCopy(),
			ctx.contender.capacityTarget.DeepCopy(),
			ctx.contender.installationTarget.DeepCopy(),
			ctx.contender.trafficTarget.DeepCopy(),

			ctx.incumbent.release.DeepCopy(),
			ctx.incumbent.capacityTarget.DeepCopy(),
			ctx.incumbent.installationTarget.DeepCopy(),
			ctx.incumbent.trafficTarget.DeepCopy(),
		}

		ctx.buildClientset(fixtures).
			buildInformerFactory().
			buildRecorder().
			buildDiscovery().
			buildController()

		rel := ctx.contender.release.DeepCopy()
		expectReleaseWaitingForCommand(ctx, rel, 0)
		execute(ctx)

		actual := shippertesting.FilterActions(ctx.clientPool.Actions())
		shippertesting.CheckActions(ctx.actions, actual, ctx.t)
		shippertesting.CheckEvents(ctx.expectedEvents, ctx.receivedEvents, ctx.t)
	}
}

func execute(ctx *context) {
	stopCh := make(chan struct{})
	defer close(stopCh)

	ctx.informerFactory.Start(stopCh)
	ctx.informerFactory.WaitForCacheSync(stopCh)

	controller := ctx.controller

	wait.PollUntil(
		10*time.Millisecond,
		func() (bool, error) { return controller.relQueue.Len() >= 1, nil },
		stopCh,
	)

	readyCh := make(chan struct{})
	go func() {
		for event := range ctx.recorder.Events {
			ctx.receivedEvents = append(ctx.receivedEvents, event)
		}
		close(readyCh)
	}()

	controller.processNextReleaseWorkItem()
	controller.processNextAppWorkItem()
	close(ctx.recorder.Events)
	<-readyCh
}

func expectReleaseWaitingForCommand(ctx *context, release *shipper.Release, step int32) {
	//TODO
}
