package release

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"k8s.io/apimachinery/pkg/util/wait"
	fakediscovery "k8s.io/client-go/discovery/fake"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/chart"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/conditions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

func init() {
	releaseutil.ConditionsShouldDiscardTimestamps = true
	conditions.StrategyConditionsShouldDiscardTimestamps = true
}

var vanguard = shipper.RolloutStrategy{
	Steps: []shipper.RolloutStrategyStep{
		{
			Name:     "staging",
			Capacity: shipper.RolloutStrategyStepValue{Incumbent: 100, Contender: 1},
			Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 100, Contender: 0},
		},
		{
			Name:     "50/50",
			Capacity: shipper.RolloutStrategyStepValue{Incumbent: 50, Contender: 50},
			Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 50, Contender: 50},
		},
		{
			Name:     "full on",
			Capacity: shipper.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
			Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
		},
	},
}

type actionfilter struct {
	verbs     []string
	resources []string
}

func (af actionfilter) Extend(ext actionfilter) actionfilter {
	verbset := make(map[string]struct{})
	newverbs := make([]string, 0)
	for _, verb := range append(af.verbs, ext.verbs...) {
		if _, ok := verbset[verb]; !ok {
			newverbs = append(newverbs, verb)
			verbset[verb] = struct{}{}
		}
	}
	resourceset := make(map[string]struct{})
	newresources := make([]string, 0)
	for _, resource := range append(af.resources, ext.resources...) {
		if _, ok := resourceset[resource]; !ok {
			newresources = append(newresources, resource)
			resourceset[resource] = struct{}{}
		}
	}
	sort.Strings(newverbs)
	sort.Strings(newresources)
	return actionfilter{
		verbs:     newverbs,
		resources: newresources,
	}
}

func (af actionfilter) IsEmpty() bool {
	return len(af.verbs) == 0 && len(af.resources) == 0
}

func (af actionfilter) DoFilter(actions []kubetesting.Action) []kubetesting.Action {
	if af.IsEmpty() {
		return actions
	}
	ignore := func(action kubetesting.Action) bool {
		for _, v := range af.verbs {
			for _, r := range af.resources {
				if action.Matches(v, r) {
					return false
				}
			}
		}

		return true
	}

	var ret []kubetesting.Action
	for _, action := range actions {
		if ignore(action) {
			continue
		}

		ret = append(ret, action)
	}

	return ret
}

type fixture struct {
	initialized     bool
	t               *testing.T
	objects         []runtime.Object
	clientset       *shipperfake.Clientset
	informerFactory shipperinformers.SharedInformerFactory
	discovery       *fakediscovery.FakeDiscovery
	clientPool      *fakedynamic.FakeClientPool
	recorder        *record.FakeRecorder

	actions        []kubetesting.Action
	filter         actionfilter
	receivedEvents []string
	expectedEvents []string
}

func newFixture(t *testing.T, objects ...runtime.Object) *fixture {
	return &fixture{
		initialized: false,
		t:           t,
		objects:     objects,

		actions:        make([]kubetesting.Action, 0),
		filter:         actionfilter{},
		receivedEvents: make([]string, 0),
		expectedEvents: make([]string, 0),
	}
}

func (f *fixture) addObjects(objects ...runtime.Object) {
	f.objects = append(f.objects, objects...)
}

func (f *fixture) init() {
	f.clientset = shipperfake.NewSimpleClientset(f.objects...)
	f.discovery = f.clientset.Discovery().(*fakediscovery.FakeDiscovery)
	f.discovery.Resources = []*metav1.APIResourceList{
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

	const syncPeriod time.Duration = 0
	informerFactory := shipperinformers.NewSharedInformerFactory(f.clientset, syncPeriod)

	f.informerFactory = informerFactory
	f.clientPool = &fakedynamic.FakeClientPool{}
	f.recorder = record.NewFakeRecorder(42)
	f.initialized = true
}

func (f *fixture) run() {
	if !f.initialized {
		f.t.Fatalf("The fixture should be initialized using init() before running it")
	}
	controller := f.newController()

	stopCh := make(chan struct{})
	defer close(stopCh)

	f.informerFactory.Start(stopCh)
	f.informerFactory.WaitForCacheSync(stopCh)

	wait.PollUntil(
		10*time.Millisecond,
		func() (bool, error) { return controller.releaseWorkqueue.Len() >= 1, nil },
		stopCh,
	)

	readyCh := make(chan struct{})
	go func() {
		for e := range f.recorder.Events {
			f.receivedEvents = append(f.receivedEvents, e)
		}
		close(readyCh)
	}()

	for controller.releaseWorkqueue.Len() > 0 {
		controller.processNextReleaseWorkItem()
	}
	for controller.applicationWorkqueue.Len() > 0 {
		controller.processNextAppWorkItem()
	}
	close(f.recorder.Events)
	<-readyCh

	actual := shippertesting.FilterActions(f.clientset.Actions())
	actual = f.filter.DoFilter(actual)

	shippertesting.CheckActions(f.actions, actual, f.t)
	shippertesting.CheckEvents(f.expectedEvents, f.receivedEvents, f.t)
}

func (f *fixture) newController() *Controller {
	return NewController(
		f.clientset,
		f.informerFactory,
		chart.FetchRemote(),
		f.clientPool,
		f.recorder,
	)
}

func buildApplication(namespace string, appName string) *shipper.Application {
	return &shipper.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appName,
			Namespace: namespace,
			UID:       "foobarbaz",
		},
		Status: shipper.ApplicationStatus{
			History: []string{},
		},
	}
}

func buildCluster(name string) *shipper.Cluster {
	return &shipper.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: shipper.ClusterSpec{
			APIMaster:    "https://127.0.0.1",
			Capabilities: []string{},
			Region:       shippertesting.TestRegion,
		},
	}
}

func (f *fixture) buildIncumbent(namespace string, relName string, replicaCount int32) *releaseInfo {
	var app *shipper.Application
	for _, object := range f.objects {
		if conv, ok := object.(*shipper.Application); ok {
			app = conv
			break
		}
	}
	if app == nil {
		f.t.Fatalf("The fixture is missing an Application object")
	}

	rel := &shipper.Release{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "Release",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      relName,
			Namespace: namespace,
			OwnerReferences: []metav1.OwnerReference{
				metav1.OwnerReference{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Kind:       "Application",
					Name:       app.GetName(),
					UID:        app.GetUID(),
				},
			},
			Labels: map[string]string{
				shipper.ReleaseLabel: relName,
				shipper.AppLabel:     app.GetName(),
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
				Chart: shipper.Chart{
					Name:    "simple",
					Version: "0.0.1",
					RepoURL: chartRepoURL,
				},
				ClusterRequirements: shipper.ClusterRequirements{
					Regions: []shipper.RegionRequirement{{Name: shippertesting.TestRegion}},
				},
			},
		},
	}

	clusterNames := make([]string, 0)
	for _, obj := range f.objects {
		if cluster, ok := obj.(*shipper.Cluster); ok {
			clusterNames = append(clusterNames, cluster.GetName())
		}
	}
	if len(clusterNames) == 0 {
		f.t.Fatalf("The fixture is missing at least 1 Cluster object")
	}

	installationTargetClusters := make([]*shipper.ClusterInstallationStatus, 0, len(clusterNames))
	for _, clusterName := range clusterNames {
		installationTargetClusters = append(installationTargetClusters, &shipper.ClusterInstallationStatus{
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
			Namespace: namespace,
			Name:      relName,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Name:       relName,
					Kind:       "Release",
					UID:        rel.GetUID(),
				},
			},
		},
		Status: shipper.InstallationTargetStatus{
			Clusters: installationTargetClusters,
		},
		Spec: shipper.InstallationTargetSpec{
			Clusters: clusterNames,
		},
	}

	capacityTargetStatusClusters := make([]shipper.ClusterCapacityStatus, 0, len(clusterNames))
	capacityTargetSpecClusters := make([]shipper.ClusterCapacityTarget, 0, len(clusterNames))
	for _, clusterName := range clusterNames {
		capacityTargetStatusClusters = append(capacityTargetStatusClusters, shipper.ClusterCapacityStatus{
			Name:            clusterName,
			AchievedPercent: 100,
		})
		capacityTargetSpecClusters = append(capacityTargetSpecClusters, shipper.ClusterCapacityTarget{
			Name:              clusterName,
			Percent:           100,
			TotalReplicaCount: replicaCount,
		})
	}

	capacityTarget := &shipper.CapacityTarget{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "CapacityTarget",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      relName,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Name:       rel.GetName(),
					Kind:       "Release",
					UID:        rel.GetUID(),
				},
			},
		},
		Status: shipper.CapacityTargetStatus{
			Clusters: capacityTargetStatusClusters,
		},
		Spec: shipper.CapacityTargetSpec{
			Clusters: capacityTargetSpecClusters,
		},
	}

	trafficTargetStatusClusters := make([]*shipper.ClusterTrafficStatus, 0, len(clusterNames))
	trafficTargetSpecClusters := make([]shipper.ClusterTrafficTarget, 0, len(clusterNames))

	for _, clusterName := range clusterNames {
		trafficTargetStatusClusters = append(trafficTargetStatusClusters, &shipper.ClusterTrafficStatus{
			Name:            clusterName,
			AchievedTraffic: 100,
		})
		trafficTargetSpecClusters = append(trafficTargetSpecClusters, shipper.ClusterTrafficTarget{
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
			Namespace: namespace,
			Name:      relName,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Name:       rel.GetName(),
					Kind:       "Release",
					UID:        rel.GetUID(),
				},
			},
		},
		Status: shipper.TrafficTargetStatus{
			Clusters: trafficTargetStatusClusters,
		},
		Spec: shipper.TrafficTargetSpec{
			Clusters: trafficTargetSpecClusters,
		},
	}

	return &releaseInfo{
		release:            rel,
		installationTarget: installationTarget,
		capacityTarget:     capacityTarget,
		trafficTarget:      trafficTarget,
	}
}

func (f *fixture) buildContender(namespace string, relName string, replicaCount int32) *releaseInfo {
	var app *shipper.Application
	for _, object := range f.objects {
		if conv, ok := object.(*shipper.Application); ok {
			app = conv
			break
		}
	}
	if app == nil {
		f.t.Fatalf("The fixture is missing an Application object")
	}

	rel := &shipper.Release{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "Release",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      relName,
			Namespace: namespace,
			OwnerReferences: []metav1.OwnerReference{
				metav1.OwnerReference{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Kind:       "Application",
					Name:       app.GetName(),
					UID:        app.GetUID(),
				},
			},
			Labels: map[string]string{
				shipper.ReleaseLabel: relName,
				shipper.AppLabel:     app.GetName(),
			},
			Annotations: map[string]string{
				shipper.ReleaseGenerationAnnotation: "1",
			},
		},
		Status: shipper.ReleaseStatus{
			Conditions: []shipper.ReleaseCondition{},
		},
		Spec: shipper.ReleaseSpec{
			TargetStep: 0,
			Environment: shipper.ReleaseEnvironment{
				Strategy: &vanguard,
				Chart: shipper.Chart{
					Name:    "simple",
					Version: "0.0.1",
					RepoURL: chartRepoURL,
				},
				ClusterRequirements: shipper.ClusterRequirements{
					Regions: []shipper.RegionRequirement{{Name: shippertesting.TestRegion}},
				},
			},
		},
	}

	clusterNames := make([]string, 0)
	for _, obj := range f.objects {
		if cluster, ok := obj.(*shipper.Cluster); ok {
			clusterNames = append(clusterNames, cluster.GetName())
		}
	}
	if len(clusterNames) == 0 {
		f.t.Fatalf("The fixture is missing at least 1 Cluster object")
	}

	installationTargetClusters := make([]*shipper.ClusterInstallationStatus, 0, len(clusterNames))
	for _, clusterName := range clusterNames {
		installationTargetClusters = append(installationTargetClusters, &shipper.ClusterInstallationStatus{
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
			Namespace: namespace,
			Name:      relName,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Name:       relName,
					Kind:       "Release",
					UID:        rel.GetUID(),
				},
			},
		},
		Status: shipper.InstallationTargetStatus{
			Clusters: installationTargetClusters,
		},
		Spec: shipper.InstallationTargetSpec{
			Clusters: clusterNames,
		},
	}

	capacityTargetStatusClusters := make([]shipper.ClusterCapacityStatus, 0, len(clusterNames))
	capacityTargetSpecClusters := make([]shipper.ClusterCapacityTarget, 0, len(clusterNames))
	for _, clusterName := range clusterNames {
		capacityTargetStatusClusters = append(capacityTargetStatusClusters, shipper.ClusterCapacityStatus{
			Name:            clusterName,
			AchievedPercent: 100,
		})
		capacityTargetSpecClusters = append(capacityTargetSpecClusters, shipper.ClusterCapacityTarget{
			Name:              clusterName,
			Percent:           0,
			TotalReplicaCount: replicaCount,
		})
	}

	capacityTarget := &shipper.CapacityTarget{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "CapacityTarget",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      relName,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Name:       rel.GetName(),
					Kind:       "Release",
					UID:        rel.GetUID(),
				},
			},
		},
		Status: shipper.CapacityTargetStatus{
			Clusters: capacityTargetStatusClusters,
		},
		Spec: shipper.CapacityTargetSpec{
			Clusters: capacityTargetSpecClusters,
		},
	}

	trafficTargetStatusClusters := make([]*shipper.ClusterTrafficStatus, 0, len(clusterNames))
	trafficTargetSpecClusters := make([]shipper.ClusterTrafficTarget, 0, len(clusterNames))

	for _, clusterName := range clusterNames {
		trafficTargetStatusClusters = append(trafficTargetStatusClusters, &shipper.ClusterTrafficStatus{
			Name:            clusterName,
			AchievedTraffic: 100,
		})
		trafficTargetSpecClusters = append(trafficTargetSpecClusters, shipper.ClusterTrafficTarget{
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
			Namespace: namespace,
			Name:      relName,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Name:       rel.GetName(),
					Kind:       "Release",
					UID:        rel.GetUID(),
				},
			},
		},
		Status: shipper.TrafficTargetStatus{
			Clusters: trafficTargetStatusClusters,
		},
		Spec: shipper.TrafficTargetSpec{
			Clusters: trafficTargetSpecClusters,
		},
	}

	return &releaseInfo{
		release:            rel,
		installationTarget: installationTarget,
		capacityTarget:     capacityTarget,
		trafficTarget:      trafficTarget,
	}
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

func randStr() string {
	var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	s := make([]rune, 16)
	for i := range s {
		s[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(s)
}

func (f *fixture) expectReleaseWaitingForCommand(rel *shipper.Release, step int32) {
	gvr := shipper.SchemeGroupVersion.WithResource("releases")
	newStatus := map[string]interface{}{
		"status": shipper.ReleaseStatus{
			AchievedStep: &shipper.AchievedStep{
				Step: step,
				Name: rel.Spec.Environment.Strategy.Steps[step].Name,
			},
			Conditions: []shipper.ReleaseCondition{
				{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
			},
			Strategy: &shipper.ReleaseStrategyStatus{
				State: shipper.ReleaseStrategyState{
					WaitingForInstallation: shipper.StrategyStateFalse,
					WaitingForCommand:      shipper.StrategyStateTrue,
					WaitingForTraffic:      shipper.StrategyStateFalse,
					WaitingForCapacity:     shipper.StrategyStateFalse,
				},
				Conditions: []shipper.ReleaseStrategyCondition{
					{
						Type:   shipper.StrategyConditionContenderAchievedCapacity,
						Status: corev1.ConditionTrue,
						Step:   step,
					},
					{
						Type:   shipper.StrategyConditionContenderAchievedInstallation,
						Status: corev1.ConditionTrue,
						Step:   step,
					},
					{
						Type:   shipper.StrategyConditionContenderAchievedTraffic,
						Status: corev1.ConditionTrue,
						Step:   step,
					},
					{
						Type:   shipper.StrategyConditionIncumbentAchievedCapacity,
						Status: corev1.ConditionTrue,
						Step:   step,
					},
					{
						Type:   shipper.StrategyConditionIncumbentAchievedTraffic,
						Status: corev1.ConditionTrue,
						Step:   step,
					},
				},
			},
		},
	}

	patch, _ := json.Marshal(newStatus)
	action := kubetesting.NewPatchAction(gvr, rel.GetNamespace(), rel.GetName(), patch)
	f.actions = append(f.actions, action)

	f.expectedEvents = []string{
		fmt.Sprintf("Normal StrategyApplied step [%d] finished", step),
		fmt.Sprintf(`Normal ReleaseStateTransitioned Release "%s/%s" had its state "WaitingForCapacity" transitioned to "False"`, rel.GetNamespace(), rel.GetName()),
		fmt.Sprintf(`Normal ReleaseStateTransitioned Release "%s/%s" had its state "WaitingForCommand" transitioned to "True"`, rel.GetNamespace(), rel.GetName()),
		fmt.Sprintf(`Normal ReleaseStateTransitioned Release "%s/%s" had its state "WaitingForInstallation" transitioned to "False"`, rel.GetNamespace(), rel.GetName()),
		fmt.Sprintf(`Normal ReleaseStateTransitioned Release "%s/%s" had its state "WaitingForTraffic" transitioned to "False"`, rel.GetNamespace(), rel.GetName()),
	}
}

func TestControllerComputeTargetClusters(t *testing.T) {
	namespace := "test-namespace"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	contenderName := randStr()
	var replicaCount int32 = 1
	contender := f.buildContender(namespace, contenderName, replicaCount)

	f.addObjects(
		contender.release.DeepCopy(),
		contender.capacityTarget.DeepCopy(),
		contender.installationTarget.DeepCopy(),
		contender.trafficTarget.DeepCopy(),
	)

	f.expectReleaseScheduled(contender.release.DeepCopy(), []*shipper.Cluster{cluster})
	f.init()
	f.run()
}

func TestControllerCreateAssociatedObjects(t *testing.T) {
	namespace := "test-namespace"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	contenderName := randStr()
	var replicaCount int32 = 1
	contender := f.buildContender(namespace, contenderName, replicaCount)

	f.addObjects(
		contender.release.DeepCopy(),
		contender.capacityTarget.DeepCopy(),
		contender.installationTarget.DeepCopy(),
		contender.trafficTarget.DeepCopy(),
	)

	f.expectAssociatedObjectsCreated(contender.release.DeepCopy(), []*shipper.Cluster{cluster})
	f.init()
	f.run()
}

func buildExpectedActions(release *shipper.Release, clusters []*shipper.Cluster) []kubetesting.Action {

	clusterNames := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		clusterNames = append(clusterNames, cluster.GetName())
	}
	sort.Strings(clusterNames)

	installationTarget := &shipper.InstallationTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: release.APIVersion,
					Kind:       release.Kind,
					Name:       release.Name,
					UID:        release.UID,
				},
			},
			Labels: map[string]string{
				shipper.AppLabel:     release.OwnerReferences[0].Name,
				shipper.ReleaseLabel: release.GetName(),
			},
		},
		Spec: shipper.InstallationTargetSpec{
			Clusters: clusterNames,
		},
	}

	clusterCapacityTargets := make([]shipper.ClusterCapacityTarget, 0, len(clusters))
	for _, cluster := range clusters {
		clusterCapacityTargets = append(
			clusterCapacityTargets,
			shipper.ClusterCapacityTarget{
				Name:              cluster.GetName(),
				Percent:           0,
				TotalReplicaCount: 12,
			})
	}

	capacityTarget := &shipper.CapacityTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.Name,
			Namespace: release.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: release.APIVersion,
					Kind:       release.Kind,
					Name:       release.Name,
					UID:        release.UID,
				},
			},
			Labels: map[string]string{
				shipper.AppLabel:     release.OwnerReferences[0].Name,
				shipper.ReleaseLabel: release.GetName(),
			},
		},
		Spec: shipper.CapacityTargetSpec{
			Clusters: clusterCapacityTargets,
		},
	}

	clusterTrafficTargets := make([]shipper.ClusterTrafficTarget, 0, len(clusters))
	for _, cluster := range clusters {
		clusterTrafficTargets = append(
			clusterTrafficTargets,
			shipper.ClusterTrafficTarget{
				Name: cluster.GetName(),
			})
	}

	trafficTarget := &shipper.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.Name,
			Namespace: release.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: release.APIVersion,
					Kind:       release.Kind,
					Name:       release.Name,
					UID:        release.UID,
				},
			},
			Labels: map[string]string{
				shipper.AppLabel:     release.OwnerReferences[0].Name,
				shipper.ReleaseLabel: release.GetName(),
			},
		},
		Spec: shipper.TrafficTargetSpec{
			Clusters: clusterTrafficTargets,
		},
	}

	actions := []kubetesting.Action{
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("installationtargets"),
			release.GetNamespace(),
			installationTarget),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("traffictargets"),
			release.GetNamespace(),
			trafficTarget),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("capacitytargets"),
			release.GetNamespace(),
			capacityTarget,
		),
	}
	return actions
}

func (f *fixture) expectAssociatedObjectsCreated(release *shipper.Release, clusters []*shipper.Cluster) {
	expected := release.DeepCopy()
	expected.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}
	f.filter = f.filter.Extend(
		actionfilter{
			[]string{"create"},
			[]string{"installationtargets", "traffictargets", "capacitytargets"},
		})
	f.actions = buildExpectedActions(expected, clusters)
	clusterNames := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		clusterNames = append(clusterNames, cluster.GetName())
	}
	f.expectedEvents = []string{
		fmt.Sprintf(
			"Normal ClustersSelected Set clusters for \"%s/%s\" to %s",
			release.GetNamespace(),
			release.GetName(),
			strings.Join(clusterNames, ","),
		),
	}
}

func (f *fixture) expectReleaseScheduled(release *shipper.Release, clusters []*shipper.Cluster) {
	clusterNames := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		clusterNames = append(clusterNames, cluster.GetName())
	}
	sort.Strings(clusterNames)
	clusterNamesStr := strings.Join(clusterNames, ",")

	expected := release.DeepCopy()
	expected.Annotations[shipper.ReleaseClustersAnnotation] = clusterNamesStr

	expectedWithConditions := expected.DeepCopy()
	condition := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&expectedWithConditions.Status, *condition)

	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expected),
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expectedWithConditions),
	}

	f.actions = append(f.actions, expectedActions...)
	f.filter = f.filter.Extend(actionfilter{[]string{"update"}, []string{"releases"}})
	f.expectedEvents = []string{
		fmt.Sprintf(
			"Normal ClustersSelected Set clusters for \"%s/%s\" to %s",
			release.GetNamespace(),
			release.GetName(),
			clusterNamesStr,
		),
	}
}

func TestContenderReleasePhaseIsWaitingForCommandForInitialStepState(t *testing.T) {
	namespace := "test-namespace"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	for _, replicaCount := range []int32{1} {
		//for _, replicaCount := range []int32{1, 3, 10} {
		incumbentName, contenderName := "test-incumbent", "test-contender"
		app.Status.History = []string{incumbentName, contenderName}
		f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
		incumbent := f.buildIncumbent(namespace, incumbentName, replicaCount)
		contender := f.buildContender(namespace, contenderName, replicaCount)

		contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
		cond := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *cond)

		contender.capacityTarget.Spec.Clusters[0].Percent = 1
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = 1
		incumbent.capacityTarget.Spec.Clusters[0].Percent = 100
		incumbent.capacityTarget.Status.Clusters[0].AvailableReplicas = replicaCount

		f.addObjects(
			incumbent.release.DeepCopy(),
			incumbent.installationTarget.DeepCopy(),
			incumbent.capacityTarget.DeepCopy(),
			incumbent.trafficTarget.DeepCopy(),
			contender.release.DeepCopy(),
			contender.installationTarget.DeepCopy(),
			contender.capacityTarget.DeepCopy(),
			contender.trafficTarget.DeepCopy(),
		)
		f.init()
		var step int32 = 0
		f.expectReleaseWaitingForCommand(contender.release.DeepCopy(), step)
		f.run()
	}
}
