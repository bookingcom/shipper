package release

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/apimachinery/pkg/util/wait"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	apputil "github.com/bookingcom/shipper/pkg/util/application"
	"github.com/bookingcom/shipper/pkg/util/conditions"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
	"github.com/bookingcom/shipper/pkg/util/replicas"
	targetutil "github.com/bookingcom/shipper/pkg/util/target"
)

func init() {
	apputil.ConditionsShouldDiscardTimestamps = true
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

type role int

const (
	Contender role = iota
	Incumbent
	testRolloutBlockName = "test-rollout-block"
)

type releaseInfoPair struct {
	incumbent *releaseInfo
	contender *releaseInfo
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
	cycles          int
	objects         []runtime.Object
	clientset       *shipperfake.Clientset
	informerFactory shipperinformers.SharedInformerFactory
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
		cycles:      -1,
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

func (f *fixture) run() {
	f.clientset = shipperfake.NewSimpleClientset(f.objects...)

	const syncPeriod time.Duration = 0
	informerFactory := shipperinformers.NewSharedInformerFactory(f.clientset, syncPeriod)

	f.informerFactory = informerFactory
	f.recorder = record.NewFakeRecorder(42)

	controller := f.newController()

	stopCh := make(chan struct{})
	defer close(stopCh)

	f.informerFactory.Start(stopCh)
	f.informerFactory.WaitForCacheSync(stopCh)

	wait.PollUntil(
		10*time.Millisecond,
		func() (bool, error) {
			return controller.releaseWorkqueue.Len() > 0, nil
		},
		stopCh,
	)

	readyCh := make(chan struct{})
	go func() {
		for e := range f.recorder.Events {
			f.receivedEvents = append(f.receivedEvents, e)
		}
		close(readyCh)
	}()

	cycles := 0
	for (f.cycles < 0 || cycles < f.cycles) && controller.releaseWorkqueue.Len() > 0 {
		controller.processNextReleaseWorkItem()
		cycles++
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
		localFetchChart,
		f.recorder,
	)
}

func newRolloutBlock(name string, namespace string) *shipper.RolloutBlock {
	return &shipper.RolloutBlock{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: shipper.RolloutBlockSpec{
			Message: "Simple test rollout block",
			Author: shipper.RolloutBlockAuthor{
				Type: "user",
				Name: "testUser",
			},
		},
	}
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

	clusterNames := make([]string, 0)
	for _, obj := range f.objects {
		if cluster, ok := obj.(*shipper.Cluster); ok {
			clusterNames = append(clusterNames, cluster.GetName())
		}
	}
	if len(clusterNames) == 0 {
		f.t.Fatalf("The fixture is missing at least 1 Cluster object")
	}

	rolloutblocksOverrides := app.Annotations[shipper.RolloutBlocksOverrideAnnotation]
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
				shipper.ReleaseGenerationAnnotation:     "0",
				shipper.ReleaseClustersAnnotation:       strings.Join(clusterNames, ","),
				shipper.RolloutBlocksOverrideAnnotation: rolloutblocksOverrides,
			},
		},
		Status: shipper.ReleaseStatus{
			AchievedStep: &shipper.AchievedStep{
				Step: 2,
				Name: "full on",
			},
			Conditions: []shipper.ReleaseCondition{
				{Type: shipper.ReleaseConditionTypeBlocked, Status: corev1.ConditionFalse},
				{Type: shipper.ReleaseConditionTypeComplete, Status: corev1.ConditionTrue},
				{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
				{Type: shipper.ReleaseConditionTypeStrategyExecuted, Status: corev1.ConditionTrue},
			},
			Strategy: &shipper.ReleaseStrategyStatus{},
		},
		Spec: shipper.ReleaseSpec{
			TargetStep: 2,
			Environment: shipper.ReleaseEnvironment{
				Strategy: &vanguard,
				Chart: shipper.Chart{
					Name:    "simple",
					Version: "0.0.1",
				},
				ClusterRequirements: shipper.ClusterRequirements{
					Regions: []shipper.RegionRequirement{{Name: shippertesting.TestRegion}},
				},
			},
		},
	}

	installationTargetClusters := make([]*shipper.ClusterInstallationStatus, 0, len(clusterNames))
	for _, clusterName := range clusterNames {
		installationTargetClusters = append(installationTargetClusters, &shipper.ClusterInstallationStatus{
			Name: clusterName,
			Conditions: []shipper.ClusterInstallationCondition{
				{Type: shipper.ClusterConditionTypeReady, Status: corev1.ConditionTrue},
			},
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
	trafficTargetStatusConditions := make([]shipper.TargetCondition, 0, len(clusterNames))
	trafficTargetSpecClusters := make([]shipper.ClusterTrafficTarget, 0, len(clusterNames))

	for _, clusterName := range clusterNames {
		trafficTargetStatusClusters = append(trafficTargetStatusClusters, &shipper.ClusterTrafficStatus{
			Name: clusterName,
			Conditions: []shipper.ClusterTrafficCondition{
				{Type: shipper.ClusterConditionTypeReady, Status: corev1.ConditionTrue},
			},
		})
		trafficTargetStatusConditions = append(trafficTargetStatusConditions, shipper.TargetCondition{
			Type:   shipper.TargetConditionTypeReady,
			Status: corev1.ConditionTrue,
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
			Clusters:   trafficTargetStatusClusters,
			Conditions: trafficTargetStatusConditions,
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

	clusterNames := make([]string, 0)
	for _, obj := range f.objects {
		if cluster, ok := obj.(*shipper.Cluster); ok {
			clusterNames = append(clusterNames, cluster.GetName())
		}
	}
	if len(clusterNames) == 0 {
		f.t.Fatalf("The fixture is missing at least 1 Cluster object")
	}

	rolloutblocksOverrides := app.Annotations[shipper.RolloutBlocksOverrideAnnotation]
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
				shipper.ReleaseGenerationAnnotation:     "1",
				shipper.RolloutBlocksOverrideAnnotation: rolloutblocksOverrides,
			},
		},
		Status: shipper.ReleaseStatus{
			Conditions: []shipper.ReleaseCondition{
				{Type: shipper.ReleaseConditionTypeBlocked, Status: corev1.ConditionFalse},
			},
			Strategy: &shipper.ReleaseStrategyStatus{},
		},
		Spec: shipper.ReleaseSpec{
			TargetStep: 0,
			Environment: shipper.ReleaseEnvironment{
				Strategy: &vanguard,
				Chart: shipper.Chart{
					Name:    "simple",
					Version: "0.0.1",
				},
				ClusterRequirements: shipper.ClusterRequirements{
					Regions: []shipper.RegionRequirement{{Name: shippertesting.TestRegion}},
				},
			},
		},
	}

	installationTargetClusters := make([]*shipper.ClusterInstallationStatus, 0, len(clusterNames))
	for _, clusterName := range clusterNames {
		installationTargetClusters = append(installationTargetClusters, &shipper.ClusterInstallationStatus{
			Name: clusterName,
			Conditions: []shipper.ClusterInstallationCondition{
				{Type: shipper.ClusterConditionTypeReady, Status: corev1.ConditionTrue},
			},
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
	trafficTargetStatusConditions := make([]shipper.TargetCondition, 0, len(clusterNames))
	trafficTargetSpecClusters := make([]shipper.ClusterTrafficTarget, 0, len(clusterNames))

	for _, clusterName := range clusterNames {
		trafficTargetStatusClusters = append(trafficTargetStatusClusters, &shipper.ClusterTrafficStatus{
			Name: clusterName,
			Conditions: []shipper.ClusterTrafficCondition{
				{Type: shipper.ClusterConditionTypeReady, Status: corev1.ConditionTrue},
			},
		})
		trafficTargetStatusConditions = append(trafficTargetStatusConditions, shipper.TargetCondition{
			Type:   shipper.TargetConditionTypeReady,
			Status: corev1.ConditionTrue,
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
			Clusters:   trafficTargetStatusClusters,
			Conditions: trafficTargetStatusConditions,
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

func addCluster(ri *releaseInfo, cluster *shipper.Cluster) {

	clusters := getReleaseClusters(ri.release)
	exists := false
	for _, cl := range clusters {
		if cl == cluster.Name {
			exists = true
			break
		}
	}
	if !exists {
		clusters = append(clusters, cluster.Name)
		sort.Strings(clusters)
		ri.release.ObjectMeta.Annotations[shipper.ReleaseClustersAnnotation] = strings.Join(clusters, ",")
	}

	ri.installationTarget.Spec.Clusters = append(ri.installationTarget.Spec.Clusters, cluster.Name)

	ri.installationTarget.Status.Clusters = append(ri.installationTarget.Status.Clusters,
		&shipper.ClusterInstallationStatus{
			Name: cluster.Name,
			Conditions: []shipper.ClusterInstallationCondition{
				{Type: shipper.ClusterConditionTypeReady, Status: corev1.ConditionTrue},
			},
		},
	)

	ri.capacityTarget.Status.Clusters = append(ri.capacityTarget.Status.Clusters,
		shipper.ClusterCapacityStatus{Name: cluster.Name, AchievedPercent: 100},
	)

	ri.capacityTarget.Spec.Clusters = append(ri.capacityTarget.Spec.Clusters,
		shipper.ClusterCapacityTarget{Name: cluster.Name, Percent: 0},
	)

	ri.trafficTarget.Spec.Clusters = append(ri.trafficTarget.Spec.Clusters,
		shipper.ClusterTrafficTarget{Name: cluster.Name, Weight: 0},
	)

	ri.trafficTarget.Status.Clusters = append(ri.trafficTarget.Status.Clusters,
		&shipper.ClusterTrafficStatus{Name: cluster.Name},
	)
}

func (f *fixture) expectReleaseWaitingForCommand(rel *shipper.Release, step int32) {
	f.filter = f.filter.Extend(actionfilter{
		[]string{"patch"},
		[]string{"releases"},
	})

	gvr := shipper.SchemeGroupVersion.WithResource("releases")
	newStatus := map[string]interface{}{
		"status": shipper.ReleaseStatus{
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
	action := kubetesting.NewPatchAction(gvr, rel.GetNamespace(), rel.GetName(), types.MergePatchType, patch)
	f.actions = append(f.actions, action)

	relKey := fmt.Sprintf("%s/%s", rel.GetNamespace(), rel.GetName())
	f.expectedEvents = []string{
		fmt.Sprintf("Normal StrategyApplied step [%d] finished", step),
		fmt.Sprintf(`Normal ReleaseStateTransitioned Release "%s" had its state "WaitingForCapacity" transitioned to "False"`, relKey),
		fmt.Sprintf(`Normal ReleaseStateTransitioned Release "%s" had its state "WaitingForCommand" transitioned to "True"`, relKey),
		fmt.Sprintf(`Normal ReleaseStateTransitioned Release "%s" had its state "WaitingForInstallation" transitioned to "False"`, relKey),
		fmt.Sprintf(`Normal ReleaseStateTransitioned Release "%s" had its state "WaitingForTraffic" transitioned to "False"`, relKey),
	}
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
			Clusters:    clusterNames,
			CanOverride: true,
			Chart:       release.Spec.Environment.Chart.DeepCopy(),
			Values:      release.Spec.Environment.Values,
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
	f.filter = f.filter.Extend(
		actionfilter{
			[]string{"create"},
			[]string{"installationtargets", "traffictargets", "capacitytargets"},
		})

	relKey := fmt.Sprintf("%s/%s", release.GetNamespace(), release.GetName())
	f.expectedEvents = append(f.expectedEvents,
		fmt.Sprintf(
			"Normal ReleaseScheduled Created InstallationTarget \"%s\"",
			relKey,
		),
		fmt.Sprintf(
			"Normal ReleaseScheduled Created TrafficTarget \"%s\"",
			relKey,
		),
		fmt.Sprintf(
			"Normal ReleaseScheduled Created CapacityTarget \"%s\"",
			relKey,
		),
		"Normal ReleaseConditionChanged [] -> [Scheduled True], [] -> [StrategyExecuted True]",
	)
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
	expected.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeBlocked, Status: corev1.ConditionFalse},
		{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
		{Type: shipper.ReleaseConditionTypeStrategyExecuted, Status: corev1.ConditionTrue},
	}

	f.filter = f.filter.Extend(actionfilter{[]string{"update"}, []string{"releases"}})
	f.actions = append(f.actions, buildExpectedActions(expected, clusters)...)
	f.actions = append(f.actions, kubetesting.NewUpdateAction(
		shipper.SchemeGroupVersion.WithResource("releases"),
		release.GetNamespace(),
		expected))

	relKey := fmt.Sprintf("%s/%s", release.GetNamespace(), release.GetName())
	f.expectedEvents = append(f.expectedEvents,
		fmt.Sprintf(
			"Normal ClustersSelected Set clusters for \"%s\" to %s",
			relKey,
			clusterNamesStr,
		),
	)
}

func (f *fixture) expectCapacityStatusPatch(ct *shipper.CapacityTarget, r *shipper.Release, value uint, totalReplicaCount uint, role role) {
	f.filter = f.filter.Extend(actionfilter{
		[]string{"patch"},
		[]string{"releases", "capacitytargets"},
	})

	gvr := shipper.SchemeGroupVersion.WithResource("capacitytargets")
	newSpec := map[string]interface{}{
		"spec": shipper.CapacityTargetSpec{
			Clusters: []shipper.ClusterCapacityTarget{
				{Name: "minikube", Percent: int32(value), TotalReplicaCount: int32(totalReplicaCount)},
			},
		},
	}
	patch, _ := json.Marshal(newSpec)
	action := kubetesting.NewPatchAction(gvr, ct.GetNamespace(), ct.GetName(), types.MergePatchType, patch)
	f.actions = append(f.actions, action)

	step := r.Spec.TargetStep

	var strategyConditions conditions.StrategyConditionsMap

	if role == Contender {
		strategyConditions = conditions.NewStrategyConditions(
			shipper.ReleaseStrategyCondition{
				Type:   shipper.StrategyConditionContenderAchievedInstallation,
				Status: corev1.ConditionTrue,
				Step:   step,
			},
			shipper.ReleaseStrategyCondition{
				Type:   shipper.StrategyConditionContenderAchievedCapacity,
				Status: corev1.ConditionFalse,
				Step:   step,
				Reason: ClustersNotReady,
				Message: fmt.Sprintf(
					"release %q hasn't achieved capacity in clusters: [%s]. for more details try `kubectl describe ct %s`",
					ct.Name,
					"minikube",
					ct.Name,
				),
			},
		)
	} else {
		strategyConditions = conditions.NewStrategyConditions(
			shipper.ReleaseStrategyCondition{
				Type:   shipper.StrategyConditionContenderAchievedInstallation,
				Status: corev1.ConditionTrue,
				Step:   step,
			},
			shipper.ReleaseStrategyCondition{
				Type:   shipper.StrategyConditionContenderAchievedCapacity,
				Status: corev1.ConditionTrue,
				Step:   step,
			},
			shipper.ReleaseStrategyCondition{
				Type:   shipper.StrategyConditionContenderAchievedTraffic,
				Status: corev1.ConditionTrue,
				Step:   step,
			},
			shipper.ReleaseStrategyCondition{
				Type:   shipper.StrategyConditionIncumbentAchievedTraffic,
				Status: corev1.ConditionTrue,
				Step:   step,
			},
			shipper.ReleaseStrategyCondition{
				Type:   shipper.StrategyConditionIncumbentAchievedCapacity,
				Status: corev1.ConditionFalse,
				Step:   step,
				Reason: ClustersNotReady,
				Message: fmt.Sprintf(
					"release %q hasn't achieved capacity in clusters: [%s]. for more details try `kubectl describe ct %s`",
					ct.Name,
					"minikube",
					ct.Name,
				),
			},
		)
	}

	newStatus := map[string]interface{}{
		"status": shipper.ReleaseStatus{
			Strategy: &shipper.ReleaseStrategyStatus{
				Conditions: strategyConditions.AsReleaseStrategyConditions(),
				State:      strategyConditions.AsReleaseStrategyState(r.Spec.TargetStep, true, false),
			},
		},
	}
	patch, _ = json.Marshal(newStatus)
	action = kubetesting.NewPatchAction(
		shipper.SchemeGroupVersion.WithResource("releases"),
		r.GetNamespace(),
		r.GetName(),
		types.MergePatchType,
		patch)
	f.actions = append(f.actions, action)

	f.expectedEvents = []string{}
}

func (f *fixture) expectTrafficStatusPatch(tt *shipper.TrafficTarget, r *shipper.Release, value uint32, role role) {
	f.filter = f.filter.Extend(actionfilter{
		[]string{"patch"},
		[]string{"releases", "traffictargets"},
	})

	gvr := shipper.SchemeGroupVersion.WithResource("traffictargets")
	newSpec := map[string]interface{}{
		"spec": shipper.TrafficTargetSpec{
			Clusters: []shipper.ClusterTrafficTarget{
				{Name: "minikube", Weight: value},
			},
		},
	}
	patch, _ := json.Marshal(newSpec)
	action := kubetesting.NewPatchAction(gvr, tt.GetNamespace(), tt.GetName(), types.MergePatchType, patch)
	f.actions = append(f.actions, action)

	step := r.Spec.TargetStep

	var strategyConditions conditions.StrategyConditionsMap

	if role == Contender {
		strategyConditions = conditions.NewStrategyConditions(
			shipper.ReleaseStrategyCondition{
				Type:   shipper.StrategyConditionContenderAchievedInstallation,
				Status: corev1.ConditionTrue,
				Step:   step,
			},
			shipper.ReleaseStrategyCondition{
				Type:   shipper.StrategyConditionContenderAchievedCapacity,
				Status: corev1.ConditionTrue,
				Step:   step,
			},
			shipper.ReleaseStrategyCondition{
				Type:   shipper.StrategyConditionContenderAchievedTraffic,
				Status: corev1.ConditionFalse,
				Step:   step,
				Reason: ClustersNotReady,
				Message: fmt.Sprintf(
					"release %q hasn't achieved traffic in clusters: [%s]. for more details try `kubectl describe tt %s`",
					tt.Name,
					"minikube",
					tt.Name,
				),
			},
		)
	} else {
		strategyConditions = conditions.NewStrategyConditions(
			shipper.ReleaseStrategyCondition{
				Type:   shipper.StrategyConditionContenderAchievedInstallation,
				Status: corev1.ConditionTrue,
				Step:   step,
			},
			shipper.ReleaseStrategyCondition{
				Type:   shipper.StrategyConditionContenderAchievedCapacity,
				Status: corev1.ConditionTrue,
				Step:   step,
			},
			shipper.ReleaseStrategyCondition{
				Type:   shipper.StrategyConditionContenderAchievedTraffic,
				Status: corev1.ConditionTrue,
				Step:   step,
			},
			shipper.ReleaseStrategyCondition{
				Type:   shipper.StrategyConditionIncumbentAchievedTraffic,
				Status: corev1.ConditionFalse,
				Step:   step,
				Reason: ClustersNotReady,
				Message: fmt.Sprintf(
					"release %q hasn't achieved traffic in clusters: [%s]. for more details try `kubectl describe tt %s`",
					tt.Name,
					"minikube",
					tt.Name,
				),
			},
		)
	}

	newStatus := map[string]interface{}{
		"status": shipper.ReleaseStatus{
			Strategy: &shipper.ReleaseStrategyStatus{
				Conditions: strategyConditions.AsReleaseStrategyConditions(),
				State:      strategyConditions.AsReleaseStrategyState(r.Spec.TargetStep, true, false),
			},
		},
	}
	patch, _ = json.Marshal(newStatus)
	action = kubetesting.NewPatchAction(
		shipper.SchemeGroupVersion.WithResource("releases"),
		r.GetNamespace(),
		r.GetName(),
		types.MergePatchType,
		patch)
	f.actions = append(f.actions, action)

	f.expectedEvents = []string{}
}

func (f *fixture) expectReleaseReleased(rel *shipper.Release, targetStep int32) {
	f.filter = f.filter.Extend(actionfilter{
		[]string{"patch"},
		[]string{"releases"},
	})

	gvr := shipper.SchemeGroupVersion.WithResource("releases")
	newStatus := map[string]interface{}{
		"status": shipper.ReleaseStatus{
			Strategy: &shipper.ReleaseStrategyStatus{
				State: shipper.ReleaseStrategyState{
					WaitingForInstallation: shipper.StrategyStateFalse,
					WaitingForCommand:      shipper.StrategyStateFalse,
					WaitingForTraffic:      shipper.StrategyStateFalse,
					WaitingForCapacity:     shipper.StrategyStateFalse,
				},
				// The following conditions are sorted alphabetically by Type
				Conditions: []shipper.ReleaseStrategyCondition{
					{
						Type:   shipper.StrategyConditionContenderAchievedCapacity,
						Status: corev1.ConditionTrue,
						Step:   targetStep,
					},
					{
						Type:   shipper.StrategyConditionContenderAchievedInstallation,
						Status: corev1.ConditionTrue,
						Step:   targetStep,
					},
					{
						Type:   shipper.StrategyConditionContenderAchievedTraffic,
						Status: corev1.ConditionTrue,
						Step:   targetStep,
					},
					{
						Type:   shipper.StrategyConditionIncumbentAchievedCapacity,
						Status: corev1.ConditionTrue,
						Step:   targetStep,
					},
					{
						Type:   shipper.StrategyConditionIncumbentAchievedTraffic,
						Status: corev1.ConditionTrue,
						Step:   targetStep,
					},
				},
			},
		},
	}

	patch, _ := json.Marshal(newStatus)
	action := kubetesting.NewPatchAction(gvr, rel.GetNamespace(), rel.GetName(), types.MergePatchType, patch)

	f.actions = append(f.actions, action)

	f.expectedEvents = []string{
		fmt.Sprintf("Normal StrategyApplied step [%d] finished", targetStep),
		fmt.Sprintf(`Normal ReleaseStateTransitioned Release "%s/%s" had its state "WaitingForCapacity" transitioned to "False"`, rel.GetNamespace(), rel.GetName()),
		fmt.Sprintf(`Normal ReleaseStateTransitioned Release "%s/%s" had its state "WaitingForCommand" transitioned to "False"`, rel.GetNamespace(), rel.GetName()),
		fmt.Sprintf(`Normal ReleaseStateTransitioned Release "%s/%s" had its state "WaitingForInstallation" transitioned to "False"`, rel.GetNamespace(), rel.GetName()),
		fmt.Sprintf(`Normal ReleaseStateTransitioned Release "%s/%s" had its state "WaitingForTraffic" transitioned to "False"`, rel.GetNamespace(), rel.GetName()),
		"Normal ReleaseConditionChanged [] -> [Complete True]",
	}
}

// NOTE(btyler): when we add tests to use this function with a wider set of use
// cases, we'll need a "pint32(int32) *int32" func to let us take pointers to literals
func (f *fixture) expectInstallationNotReady(rel *shipper.Release, achievedStepIndex *int32, targetStepIndex int32, role role) {
	f.filter = f.filter.Extend(actionfilter{
		[]string{"patch"},
		[]string{"releases"},
	})

	gvr := shipper.SchemeGroupVersion.WithResource("releases")

	// var achievedStep *shipper.AchievedStep
	// if achievedStepIndex != nil {
	// 	achievedStep = &shipper.AchievedStep{
	// 		Step: *achievedStepIndex,
	// 		Name: rel.Spec.Environment.Strategy.Steps[*achievedStepIndex].Name,
	// 	}
	// }

	newStatus := map[string]interface{}{
		"status": shipper.ReleaseStatus{
			Strategy: &shipper.ReleaseStrategyStatus{
				State: shipper.ReleaseStrategyState{
					WaitingForInstallation: shipper.StrategyStateTrue,
					WaitingForCommand:      shipper.StrategyStateFalse,
					WaitingForTraffic:      shipper.StrategyStateFalse,
					WaitingForCapacity:     shipper.StrategyStateFalse,
				},
				Conditions: []shipper.ReleaseStrategyCondition{
					{
						Type:    shipper.StrategyConditionContenderAchievedInstallation,
						Status:  corev1.ConditionFalse,
						Reason:  ClustersNotReady,
						Step:    targetStepIndex,
						Message: fmt.Sprintf("clusters pending installation: [broken-installation-cluster]. for more details try `kubectl describe it %s`", rel.Name),
					},
				},
			},
		},
	}

	patch, _ := json.Marshal(newStatus)
	action := kubetesting.NewPatchAction(gvr, rel.GetNamespace(), rel.GetName(), types.MergePatchType, patch)

	f.actions = append(f.actions, action)

	f.expectedEvents = []string{}
}

func (f *fixture) expectCapacityNotReady(relpair releaseInfoPair, targetStep, achievedStepIndex int32, role role, brokenClusterName string) {
	gvr := shipper.SchemeGroupVersion.WithResource("releases")
	rel := relpair.contender.release

	var newStatus map[string]interface{}

	// var achievedStep *shipper.AchievedStep
	// if achievedStepIndex != 0 {
	// 	achievedStep = &shipper.AchievedStep{
	// 		Step: achievedStepIndex,
	// 		Name: rel.Spec.Environment.Strategy.Steps[achievedStepIndex].Name,
	// 	}
	// }

	if role == Contender {
		newStatus = map[string]interface{}{
			"status": shipper.ReleaseStatus{
				Strategy: &shipper.ReleaseStrategyStatus{
					State: shipper.ReleaseStrategyState{
						WaitingForInstallation: shipper.StrategyStateFalse,
						WaitingForCommand:      shipper.StrategyStateFalse,
						WaitingForTraffic:      shipper.StrategyStateFalse,
						WaitingForCapacity:     shipper.StrategyStateTrue,
					},
					Conditions: []shipper.ReleaseStrategyCondition{
						{
							Type:   shipper.StrategyConditionContenderAchievedCapacity,
							Status: corev1.ConditionFalse,
							Reason: ClustersNotReady,
							Message: fmt.Sprintf(
								"release %q hasn't achieved capacity in clusters: [%s]. for more details try `kubectl describe ct %s`",
								relpair.contender.release.Name,
								brokenClusterName,
								relpair.contender.capacityTarget.Name,
							),
							Step: targetStep,
						},
						{
							Type:   shipper.StrategyConditionContenderAchievedInstallation,
							Status: corev1.ConditionTrue,
							Step:   targetStep,
						},
					},
				},
			},
		}
	} else {
		newStatus = map[string]interface{}{
			"status": shipper.ReleaseStatus{
				Strategy: &shipper.ReleaseStrategyStatus{
					State: shipper.ReleaseStrategyState{
						WaitingForInstallation: shipper.StrategyStateFalse,
						WaitingForCommand:      shipper.StrategyStateFalse,
						WaitingForTraffic:      shipper.StrategyStateFalse,
						WaitingForCapacity:     shipper.StrategyStateTrue,
					},
					Conditions: []shipper.ReleaseStrategyCondition{
						{
							Type:   shipper.StrategyConditionContenderAchievedCapacity,
							Status: corev1.ConditionTrue,
							Step:   targetStep,
						},
						{
							Type:   shipper.StrategyConditionContenderAchievedInstallation,
							Status: corev1.ConditionTrue,
							Step:   targetStep,
						},
						{
							Type:   shipper.StrategyConditionContenderAchievedTraffic,
							Status: corev1.ConditionTrue,
							Step:   targetStep,
						},
						{
							Type:   shipper.StrategyConditionIncumbentAchievedCapacity,
							Status: corev1.ConditionFalse,
							Reason: ClustersNotReady,
							Step:   targetStep,
							Message: fmt.Sprintf(
								"release %q hasn't achieved capacity in clusters: [%s]. for more details try `kubectl describe ct %s`",
								relpair.incumbent.release.Name,
								brokenClusterName,
								relpair.incumbent.capacityTarget.Name,
							),
						},
						{
							Type:   shipper.StrategyConditionIncumbentAchievedTraffic,
							Status: corev1.ConditionTrue,
							Step:   targetStep,
						},
					},
				},
			},
		}
	}

	patch, _ := json.Marshal(newStatus)
	action := kubetesting.NewPatchAction(gvr, rel.GetNamespace(), rel.GetName(), types.MergePatchType, patch)

	f.actions = append(f.actions, action)

	f.expectedEvents = []string{}
}

func (f *fixture) expectTrafficNotReady(relpair releaseInfoPair, targetStep, achievedStepIndex int32, role role, brokenClusterName string) {
	gvr := shipper.SchemeGroupVersion.WithResource("releases")
	rel := relpair.contender.release
	var newStatus map[string]interface{}

	// var achievedStep *shipper.AchievedStep
	// if achievedStepIndex != 0 {
	// 	achievedStep = &shipper.AchievedStep{
	// 		Step: achievedStepIndex,
	// 		Name: rel.Spec.Environment.Strategy.Steps[achievedStepIndex].Name,
	// 	}
	// }

	if role == Contender {
		newStatus = map[string]interface{}{
			"status": shipper.ReleaseStatus{
				Strategy: &shipper.ReleaseStrategyStatus{
					State: shipper.ReleaseStrategyState{
						WaitingForInstallation: shipper.StrategyStateFalse,
						WaitingForCommand:      shipper.StrategyStateFalse,
						WaitingForTraffic:      shipper.StrategyStateTrue,
						WaitingForCapacity:     shipper.StrategyStateFalse,
					},
					Conditions: []shipper.ReleaseStrategyCondition{
						{
							Type:   shipper.StrategyConditionContenderAchievedCapacity,
							Status: corev1.ConditionTrue,
							Step:   targetStep,
						},
						{
							Type:   shipper.StrategyConditionContenderAchievedInstallation,
							Status: corev1.ConditionTrue,
							Step:   targetStep,
						},
						{
							Type:   shipper.StrategyConditionContenderAchievedTraffic,
							Status: corev1.ConditionFalse,
							Reason: ClustersNotReady,
							Message: fmt.Sprintf(
								"release %q hasn't achieved traffic in clusters: [%s]. for more details try `kubectl describe tt %s`",
								relpair.contender.release.Name,
								brokenClusterName,
								relpair.contender.capacityTarget.Name,
							),
							Step: targetStep,
						},
					},
				},
			},
		}
	} else {
		newStatus = map[string]interface{}{
			"status": shipper.ReleaseStatus{
				Strategy: &shipper.ReleaseStrategyStatus{
					State: shipper.ReleaseStrategyState{
						WaitingForInstallation: shipper.StrategyStateFalse,
						WaitingForCommand:      shipper.StrategyStateFalse,
						WaitingForTraffic:      shipper.StrategyStateTrue,
						WaitingForCapacity:     shipper.StrategyStateFalse,
					},
					Conditions: []shipper.ReleaseStrategyCondition{
						{
							Type:   shipper.StrategyConditionContenderAchievedCapacity,
							Status: corev1.ConditionTrue,
							Step:   targetStep,
						},
						{
							Type:   shipper.StrategyConditionContenderAchievedInstallation,
							Status: corev1.ConditionTrue,
							Step:   targetStep,
						},
						{
							Type:   shipper.StrategyConditionContenderAchievedTraffic,
							Status: corev1.ConditionTrue,
							Step:   targetStep,
						},
						{
							Type:   shipper.StrategyConditionIncumbentAchievedTraffic,
							Status: corev1.ConditionFalse,
							Reason: ClustersNotReady,
							Message: fmt.Sprintf(
								"release %q hasn't achieved traffic in clusters: [%s]. for more details try `kubectl describe tt %s`",
								relpair.incumbent.release.Name,
								brokenClusterName,
								relpair.incumbent.capacityTarget.Name,
							),
							Step: targetStep,
						},
					},
				},
			},
		}
	}

	patch, _ := json.Marshal(newStatus)
	action := kubetesting.NewPatchAction(gvr, rel.GetNamespace(), rel.GetName(), types.MergePatchType, patch)

	f.actions = append(f.actions, action)

	f.expectedEvents = []string{}
}

func TestControllerComputeTargetClustersAndCreateAssociatedObjects(t *testing.T) {
	namespace := "test-namespace"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	f.cycles = 1
	contenderName := "test-contender"
	var replicaCount int32 = 1
	contender := f.buildContender(namespace, contenderName, replicaCount)

	f.addObjects(
		contender.release.DeepCopy(),
	)

	f.expectReleaseScheduled(contender.release, []*shipper.Cluster{cluster})
	f.expectAssociatedObjectsCreated(contender.release, []*shipper.Cluster{cluster})

	f.run()
}

func TestContenderReleasePhaseIsWaitingForCommandForInitialStepState(t *testing.T) {
	namespace := "test-namespace"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	for _, replicaCount := range []int32{1, 3, 10} {
		incumbentName, contenderName := "test-incumbent", "test-contender"
		app.Status.History = []string{incumbentName, contenderName}
		f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
		incumbent := f.buildIncumbent(namespace, incumbentName, replicaCount)
		contender := f.buildContender(namespace, contenderName, replicaCount)

		contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
		condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
		condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

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
		var step int32 = 0
		f.expectReleaseWaitingForCommand(contender.release, step)
		f.run()
	}
}

func TestContenderDoNothingClusterInstallationNotReady(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")
	brokenCluster := buildCluster("broken-installation-cluster")

	for _, totalReplicaCount := range []int32{1, 3, 10} {
		f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())

		contender := f.buildContender(namespace, contenderName, totalReplicaCount)
		incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

		contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
		condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
		condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

		addCluster(contender, brokenCluster)

		contender.release.Spec.TargetStep = 0

		incumbent.capacityTarget.Spec.Clusters[0].Percent = 100
		incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
		incumbent.capacityTarget.Status.Clusters[0].AchievedPercent = 100
		incumbent.capacityTarget.Status.Clusters[0].AvailableReplicas = totalReplicaCount
		incumbent.trafficTarget.Spec.Clusters[0].Weight = 100
		incumbent.trafficTarget.Status.Clusters[0].AchievedTraffic = 100

		// the fixture creates installation targets in 'installation succeeded' status,
		// so we'll break one
		//contender.installationTarget.Status.Clusters[1].Status = shipper.InstallationStatusFailed
		contender.installationTarget.Status.Clusters[1].Conditions = []shipper.ClusterInstallationCondition{}

		f.addObjects(
			contender.release.DeepCopy(),
			contender.installationTarget.DeepCopy(),
			contender.capacityTarget.DeepCopy(),
			contender.trafficTarget.DeepCopy(),

			incumbent.release.DeepCopy(),
			incumbent.installationTarget.DeepCopy(),
			incumbent.capacityTarget.DeepCopy(),
			incumbent.trafficTarget.DeepCopy(),
		)

		r := contender.release.DeepCopy()
		f.expectInstallationNotReady(r, nil, 0, Contender)
		f.run()
	}
}

func TestContenderDoNothingClusterCapacityNotReady(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")
	brokenCluster := buildCluster("broken-capacity-cluster")

	for _, totalReplicaCount := range []int32{1, 3, 10} {
		f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
		f.cycles = 1

		contender := f.buildContender(namespace, contenderName, totalReplicaCount)
		incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

		contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
		condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
		condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

		addCluster(contender, brokenCluster)

		// We'll set cluster 0 to be all set, but make cluster 1 broken.
		contender.release.Spec.TargetStep = 1
		contender.capacityTarget.Spec.Clusters[0].Percent = 50
		contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = int32(totalReplicaCount)
		contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))
		contender.trafficTarget.Spec.Clusters[0].Weight = 50

		// No capacity yet.
		contender.capacityTarget.Spec.Clusters[1].Percent = 50
		contender.capacityTarget.Spec.Clusters[1].TotalReplicaCount = totalReplicaCount
		contender.capacityTarget.Status.Clusters[1].AchievedPercent = 0
		contender.capacityTarget.Status.Clusters[1].AvailableReplicas = 0
		contender.trafficTarget.Spec.Clusters[1].Weight = 50

		incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
		incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
		incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
		incumbent.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		incumbent.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))

		f.addObjects(
			brokenCluster.DeepCopy(),

			contender.release.DeepCopy(),
			contender.installationTarget.DeepCopy(),
			contender.capacityTarget.DeepCopy(),
			contender.trafficTarget.DeepCopy(),

			incumbent.release.DeepCopy(),
			incumbent.installationTarget.DeepCopy(),
			incumbent.capacityTarget.DeepCopy(),
			incumbent.trafficTarget.DeepCopy(),
		)

		relpair := releaseInfoPair{
			contender: contender,
			incumbent: incumbent,
		}
		f.expectCapacityNotReady(relpair, 1, 0, Contender, brokenCluster.Name)
		f.run()
	}
}

func TestContenderDoNothingClusterTrafficNotReady(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")
	brokenCluster := buildCluster("broken-traffic-cluster")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
	condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
	condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

	addCluster(contender, brokenCluster)

	// We'll set cluster 0 to be all set, but make cluster 1 broken.
	contender.release.Spec.TargetStep = 1
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
	contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
	contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))
	contender.trafficTarget.Spec.Clusters[0].Weight = 50

	contender.capacityTarget.Spec.Clusters[1].Percent = 50
	contender.capacityTarget.Spec.Clusters[1].TotalReplicaCount = totalReplicaCount
	contender.capacityTarget.Status.Clusters[1].AchievedPercent = 50
	contender.capacityTarget.Status.Clusters[1].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))

	contender.trafficTarget.Spec.Clusters[1].Weight = 50
	// No traffic yet.
	contender.trafficTarget.Status.Conditions, _ = targetutil.SetTargetCondition(
		contender.trafficTarget.Status.Conditions,
		targetutil.NewTargetCondition(
			shipper.TargetConditionTypeReady,
			corev1.ConditionFalse,
			ClustersNotReady, "[broken-traffic-cluster]"))

	incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
	incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
	incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
	incumbent.capacityTarget.Status.Clusters[0].AchievedPercent = 50
	incumbent.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))

	f.addObjects(
		contender.release.DeepCopy(),
		contender.installationTarget.DeepCopy(),
		contender.capacityTarget.DeepCopy(),
		contender.trafficTarget.DeepCopy(),

		incumbent.release.DeepCopy(),
		incumbent.installationTarget.DeepCopy(),
		incumbent.capacityTarget.DeepCopy(),
		incumbent.trafficTarget.DeepCopy(),
	)

	relpair := releaseInfoPair{
		contender: contender,
		incumbent: incumbent,
	}
	f.expectTrafficNotReady(relpair, 1, 0, Contender, brokenCluster.Name)
	f.run()
}

func TestContenderCapacityShouldIncrease(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	for _, i := range []int32{1, 3, 10} {
		f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
		f.cycles = 1

		totalReplicaCount := i
		contender := f.buildContender(namespace, contenderName, totalReplicaCount)
		incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

		contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
		condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
		condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

		contender.release.Spec.TargetStep = 1

		incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
		incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
		incumbent.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		incumbent.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))
		incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
		incumbent.trafficTarget.Status.Clusters[0].AchievedTraffic = 50

		f.addObjects(
			contender.release.DeepCopy(),
			contender.installationTarget.DeepCopy(),
			contender.capacityTarget.DeepCopy(),
			contender.trafficTarget.DeepCopy(),

			incumbent.release.DeepCopy(),
			incumbent.installationTarget.DeepCopy(),
			incumbent.capacityTarget.DeepCopy(),
			incumbent.trafficTarget.DeepCopy(),
		)

		ct := contender.capacityTarget.DeepCopy()
		r := contender.release.DeepCopy()
		f.expectCapacityStatusPatch(ct, r, 50, uint(totalReplicaCount), Contender)
		f.run()
	}
}

func TestContenderCapacityShouldIncreaseWithRolloutBlockOverride(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	rolloutBlock := newRolloutBlock(testRolloutBlockName, namespace)
	rolloutBlockKey := fmt.Sprintf("%s/%s", namespace, testRolloutBlockName)
	cluster := buildCluster("minikube")

	for _, i := range []int32{1, 3, 10} {
		totalReplicaCount := int32(i)
		f := newFixture(t, app.DeepCopy(), cluster.DeepCopy(), rolloutBlock.DeepCopy())
		f.cycles = 1

		contender := f.buildContender(namespace, contenderName, totalReplicaCount)
		contender.release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = rolloutBlockKey
		incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)
		incumbent.release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = rolloutBlockKey

		contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
		condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
		condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

		contender.release.Spec.TargetStep = 1

		incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
		incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
		incumbent.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		incumbent.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))
		incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
		incumbent.trafficTarget.Status.Clusters[0].AchievedTraffic = 50

		f.addObjects(
			contender.release.DeepCopy(),
			contender.installationTarget.DeepCopy(),
			contender.capacityTarget.DeepCopy(),
			contender.trafficTarget.DeepCopy(),

			incumbent.release.DeepCopy(),
			incumbent.installationTarget.DeepCopy(),
			incumbent.capacityTarget.DeepCopy(),
			incumbent.trafficTarget.DeepCopy(),
		)

		ct := contender.capacityTarget.DeepCopy()
		r := contender.release.DeepCopy()
		f.expectCapacityStatusPatch(ct, r, 50, uint(totalReplicaCount), Contender)
		overrideEvent := fmt.Sprintf("%s RolloutBlockOverridden %s", corev1.EventTypeNormal, rolloutBlockKey)
		f.expectedEvents = append(f.expectedEvents, overrideEvent)
		f.run()
	}
}

func TestContenderCapacityShouldNotIncreaseWithRolloutBlock(t *testing.T) {
	namespace := "test-namespace"
	contenderName := "test-contender-bimbambom"
	app := buildApplication(namespace, "test-app")
	rolloutBlock := newRolloutBlock(testRolloutBlockName, namespace)
	rolloutBlockKey := fmt.Sprintf("%s/%s", namespace, testRolloutBlockName)
	cluster := buildCluster("minikube")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy(), rolloutBlock.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(3)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	contender.release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = ""

	contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
	condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
	condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

	contender.release.Spec.TargetStep = 1

	f.addObjects(
		contender.release.DeepCopy(),
		contender.installationTarget.DeepCopy(),
		contender.capacityTarget.DeepCopy(),
		contender.trafficTarget.DeepCopy(),
	)

	expectedContender := contender.release.DeepCopy()
	rolloutBlockMessage := fmt.Sprintf("rollout block(s) with name(s) %s exist", rolloutBlockKey)
	condBlocked := releaseutil.NewReleaseCondition(
		shipper.ReleaseConditionTypeBlocked,
		corev1.ConditionTrue,
		"RolloutsBlocked",
		rolloutBlockMessage)
	releaseutil.SetReleaseCondition(&expectedContender.Status, *condBlocked)

	action := kubetesting.NewUpdateAction(
		shipper.SchemeGroupVersion.WithResource("releases"),
		namespace,
		expectedContender)
	f.actions = append(f.actions, action)

	f.expectedEvents = append(f.expectedEvents,
		fmt.Sprintf("%s RolloutBlocked %s", corev1.EventTypeWarning, rolloutBlockKey),
		fmt.Sprintf("Normal ReleaseConditionChanged [Blocked False] -> [Blocked True RolloutsBlocked %s]", rolloutBlockMessage),
	)
	f.run()
}

func TestContenderTrafficShouldIncrease(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	for _, totalReplicaCount := range []int32{1, 3, 10} {
		f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
		f.cycles = 1 // It only runs a single cycle of processNextReleaseWorkItem

		contender := f.buildContender(namespace, contenderName, totalReplicaCount)
		incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

		contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
		condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
		condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

		contender.release.Spec.TargetStep = 1
		contender.capacityTarget.Spec.Clusters[0].Percent = 50
		contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
		contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))

		incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
		incumbent.trafficTarget.Status.Clusters[0].AchievedTraffic = 50
		incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
		incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
		incumbent.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		incumbent.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))

		f.addObjects(
			contender.release.DeepCopy(),
			contender.installationTarget.DeepCopy(),
			contender.capacityTarget.DeepCopy(),
			contender.trafficTarget.DeepCopy(),

			incumbent.release.DeepCopy(),
			incumbent.installationTarget.DeepCopy(),
			incumbent.capacityTarget.DeepCopy(),
			incumbent.trafficTarget.DeepCopy(),
		)

		tt := contender.trafficTarget.DeepCopy()
		r := contender.release.DeepCopy()
		f.expectTrafficStatusPatch(tt, r, 50, Contender)
		f.run()
	}
}

func TestContenderTrafficShouldIncreaseWithRolloutBlockOverride(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	rolloutBlock := newRolloutBlock(testRolloutBlockName, namespace)
	rolloutBlockKey := fmt.Sprintf("%s/%s", namespace, testRolloutBlockName)
	cluster := buildCluster("minikube")

	for _, totalReplicaCount := range []int32{1, 3, 10} {
		f := newFixture(t, app.DeepCopy(), cluster.DeepCopy(), rolloutBlock.DeepCopy())
		f.cycles = 1

		contender := f.buildContender(namespace, contenderName, totalReplicaCount)
		incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

		contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
		contender.release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = rolloutBlockKey
		incumbent.release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = rolloutBlockKey
		condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
		condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

		contender.release.Spec.TargetStep = 1
		contender.capacityTarget.Spec.Clusters[0].Percent = 50
		contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
		contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))

		incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
		incumbent.trafficTarget.Status.Clusters[0].AchievedTraffic = 50
		incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
		incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
		incumbent.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		incumbent.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))

		f.addObjects(
			contender.release.DeepCopy(),
			contender.installationTarget.DeepCopy(),
			contender.capacityTarget.DeepCopy(),
			contender.trafficTarget.DeepCopy(),

			incumbent.release.DeepCopy(),
			incumbent.installationTarget.DeepCopy(),
			incumbent.capacityTarget.DeepCopy(),
			incumbent.trafficTarget.DeepCopy(),
		)

		tt := contender.trafficTarget.DeepCopy()
		r := contender.release.DeepCopy()
		f.expectTrafficStatusPatch(tt, r, 50, Contender)
		overrideEvent := fmt.Sprintf("%s RolloutBlockOverridden %s", corev1.EventTypeNormal, rolloutBlockKey)
		f.expectedEvents = append(f.expectedEvents, overrideEvent)
		f.run()
	}
}

func TestContenderTrafficShouldNotIncreaseWithRolloutBlock(t *testing.T) {
	namespace := "test-namespace"
	app := buildApplication(namespace, "test-app")
	rolloutBlock := newRolloutBlock(testRolloutBlockName, namespace)
	rolloutBlockKey := fmt.Sprintf("%s/%s", namespace, testRolloutBlockName)
	cluster := buildCluster("minikube")
	contenderName := "contender"

	for _, totalReplicaCount := range []int32{1, 3, 10} {
		f := newFixture(t, app.DeepCopy(), cluster.DeepCopy(), rolloutBlock.DeepCopy())
		f.cycles = 1

		contender := f.buildContender(namespace, contenderName, totalReplicaCount)

		contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
		condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
		condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

		contender.release.Spec.TargetStep = 1
		contender.capacityTarget.Spec.Clusters[0].Percent = 50
		contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
		contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))

		f.addObjects(
			contender.release.DeepCopy(),
			contender.installationTarget.DeepCopy(),
			contender.capacityTarget.DeepCopy(),
			contender.trafficTarget.DeepCopy(),
		)

		expectedContender := contender.release.DeepCopy()
		rolloutBlockMessage := fmt.Sprintf("rollout block(s) with name(s) %s exist", rolloutBlockKey)
		condBlocked := releaseutil.NewReleaseCondition(
			shipper.ReleaseConditionTypeBlocked,
			corev1.ConditionTrue,
			"RolloutsBlocked",
			rolloutBlockMessage)
		releaseutil.SetReleaseCondition(&expectedContender.Status, *condBlocked)

		action := kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			namespace,
			expectedContender)
		f.actions = append(f.actions, action)

		f.expectedEvents = append(f.expectedEvents,
			fmt.Sprintf("%s RolloutBlocked %s", corev1.EventTypeWarning, rolloutBlockKey),
			fmt.Sprintf("Normal ReleaseConditionChanged [Blocked False] -> [Blocked True RolloutsBlocked %s]", rolloutBlockMessage))
		f.run()
	}
}

func TestIncumbentTrafficShouldDecrease(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	for _, totalReplicaCount := range []int32{1, 3, 10} {
		f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
		f.cycles = 1

		contender := f.buildContender(namespace, contenderName, totalReplicaCount)
		incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

		contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
		condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
		condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

		contender.release.Spec.TargetStep = 1
		contender.capacityTarget.Spec.Clusters[0].Percent = 50
		contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
		contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))
		contender.trafficTarget.Spec.Clusters[0].Weight = 50

		f.addObjects(
			contender.release.DeepCopy(),
			contender.installationTarget.DeepCopy(),
			contender.capacityTarget.DeepCopy(),
			contender.trafficTarget.DeepCopy(),

			incumbent.release.DeepCopy(),
			incumbent.installationTarget.DeepCopy(),
			incumbent.capacityTarget.DeepCopy(),
			incumbent.trafficTarget.DeepCopy(),
		)

		tt := incumbent.trafficTarget.DeepCopy()
		r := contender.release.DeepCopy()
		f.expectTrafficStatusPatch(tt, r, 50, Incumbent)
		f.run()
	}
}

func TestIncumbentTrafficShouldDecreaseWithRolloutBlockOverride(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	rolloutBlock := newRolloutBlock(testRolloutBlockName, namespace)
	rolloutBlockKey := fmt.Sprintf("%s/%s", namespace, testRolloutBlockName)
	cluster := buildCluster("minikube")

	for _, totalReplicaCount := range []int32{1, 3, 10} {
		f := newFixture(t, app.DeepCopy(), cluster.DeepCopy(), rolloutBlock.DeepCopy())
		f.cycles = 1 // we're looking at a single-step progression

		contender := f.buildContender(namespace, contenderName, totalReplicaCount)
		incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

		contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
		contender.release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = rolloutBlockKey
		incumbent.release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = rolloutBlockKey
		condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
		condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

		contender.release.Spec.TargetStep = 1
		contender.capacityTarget.Spec.Clusters[0].Percent = 50
		contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
		contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))
		contender.trafficTarget.Spec.Clusters[0].Weight = 50
		contender.trafficTarget.Status.Clusters[0].AchievedTraffic = 50

		f.addObjects(
			contender.release.DeepCopy(),
			contender.installationTarget.DeepCopy(),
			contender.capacityTarget.DeepCopy(),
			contender.trafficTarget.DeepCopy(),

			incumbent.release.DeepCopy(),
			incumbent.installationTarget.DeepCopy(),
			incumbent.capacityTarget.DeepCopy(),
			incumbent.trafficTarget.DeepCopy(),
		)

		tt := incumbent.trafficTarget.DeepCopy()
		r := contender.release.DeepCopy()
		f.expectTrafficStatusPatch(tt, r, 50, Incumbent)
		overrideEvent := fmt.Sprintf("%s RolloutBlockOverridden %s", corev1.EventTypeNormal, rolloutBlockKey)
		f.expectedEvents = append(f.expectedEvents, overrideEvent)
		f.run()
	}
}

func TestIncumbentTrafficShouldNotDecreaseWithRolloutBlock(t *testing.T) {
	namespace := "test-namespace"
	app := buildApplication(namespace, "test-app")
	rolloutBlock := newRolloutBlock(testRolloutBlockName, namespace)
	rolloutBlockKey := fmt.Sprintf("%s/%s", namespace, testRolloutBlockName)
	cluster := buildCluster("minikube")

	totalReplicaCount := int32(3)
	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy(), rolloutBlock.DeepCopy())
	f.cycles = 1

	contenderName := "contender"
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)

	contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
	condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
	condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

	contender.release.Spec.TargetStep = 1
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
	contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
	contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))
	contender.trafficTarget.Spec.Clusters[0].Weight = 50

	f.addObjects(
		contender.release.DeepCopy(),
		contender.installationTarget.DeepCopy(),
		contender.capacityTarget.DeepCopy(),
		contender.trafficTarget.DeepCopy(),
	)

	expectedContender := contender.release.DeepCopy()
	rolloutBlockMessage := fmt.Sprintf("rollout block(s) with name(s) %s exist", rolloutBlockKey)
	condBlocked := releaseutil.NewReleaseCondition(
		shipper.ReleaseConditionTypeBlocked,
		corev1.ConditionTrue,
		"RolloutsBlocked",
		rolloutBlockMessage)
	releaseutil.SetReleaseCondition(&expectedContender.Status, *condBlocked)

	action := kubetesting.NewUpdateAction(
		shipper.SchemeGroupVersion.WithResource("releases"),
		namespace,
		expectedContender)
	f.actions = append(f.actions, action)

	f.expectedEvents = append(f.expectedEvents,
		fmt.Sprintf("%s RolloutBlocked %s", corev1.EventTypeWarning, rolloutBlockKey),
		fmt.Sprintf("Normal ReleaseConditionChanged [Blocked False] -> [Blocked True RolloutsBlocked %s]", rolloutBlockMessage))
	f.run()
}

func TestIncumbentCapacityShouldDecrease(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	for _, totalReplicaCount := range []int32{1, 3, 10} {
		f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
		f.cycles = 1

		contender := f.buildContender(namespace, contenderName, totalReplicaCount)
		incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

		contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
		condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
		condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

		contender.release.Spec.TargetStep = 1
		contender.capacityTarget.Spec.Clusters[0].Percent = 50
		contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
		contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))
		contender.trafficTarget.Spec.Clusters[0].Weight = 50

		incumbent.trafficTarget.Spec.Clusters[0].Weight = 50

		f.addObjects(
			contender.release.DeepCopy(),
			contender.installationTarget.DeepCopy(),
			contender.capacityTarget.DeepCopy(),
			contender.trafficTarget.DeepCopy(),

			incumbent.release.DeepCopy(),
			incumbent.installationTarget.DeepCopy(),
			incumbent.capacityTarget.DeepCopy(),
			incumbent.trafficTarget.DeepCopy(),
		)

		tt := incumbent.capacityTarget.DeepCopy()
		r := contender.release.DeepCopy()
		f.expectCapacityStatusPatch(tt, r, 50, uint(totalReplicaCount), Incumbent)
		f.run()
	}
}

func TestIncumbentCapacityShouldDecreaseWithRolloutBlockOverride(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	rolloutBlock := newRolloutBlock(testRolloutBlockName, namespace)
	rolloutBlockKey := fmt.Sprintf("%s/%s", namespace, testRolloutBlockName)
	cluster := buildCluster("minikube")

	for _, totalReplicaCount := range []int32{1, 3, 10} {
		f := newFixture(t, app.DeepCopy(), cluster.DeepCopy(), rolloutBlock.DeepCopy())
		f.cycles = 1

		contender := f.buildContender(namespace, contenderName, totalReplicaCount)
		incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

		contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
		contender.release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = rolloutBlockKey
		incumbent.release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = rolloutBlockKey
		condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
		condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

		contender.release.Spec.TargetStep = 1
		contender.capacityTarget.Spec.Clusters[0].Percent = 50
		contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
		contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))
		contender.trafficTarget.Spec.Clusters[0].Weight = 50
		contender.trafficTarget.Status.Clusters[0].AchievedTraffic = 50

		incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
		incumbent.trafficTarget.Status.Clusters[0].AchievedTraffic = 50

		f.addObjects(
			contender.release.DeepCopy(),
			contender.installationTarget.DeepCopy(),
			contender.capacityTarget.DeepCopy(),
			contender.trafficTarget.DeepCopy(),

			incumbent.release.DeepCopy(),
			incumbent.installationTarget.DeepCopy(),
			incumbent.capacityTarget.DeepCopy(),
			incumbent.trafficTarget.DeepCopy(),
		)

		tt := incumbent.capacityTarget.DeepCopy()
		r := contender.release.DeepCopy()
		f.expectCapacityStatusPatch(tt, r, 50, uint(totalReplicaCount), Incumbent)
		overrideEvent := fmt.Sprintf("%s RolloutBlockOverridden %s", corev1.EventTypeNormal, rolloutBlockKey)
		f.expectedEvents = append(f.expectedEvents, overrideEvent)
		f.run()
	}
}

func TestIncumbentCapacityShouldNotDecreaseWithRolloutBlock(t *testing.T) {
	namespace := "test-namespace"
	contenderName := "test-contender"
	app := buildApplication(namespace, "test-app")
	rolloutBlock := newRolloutBlock(testRolloutBlockName, namespace)
	rolloutBlockKey := fmt.Sprintf("%s/%s", namespace, testRolloutBlockName)
	cluster := buildCluster("minikube")

	for _, totalReplicaCount := range []int32{1, 3, 10} {
		f := newFixture(t, app.DeepCopy(), cluster.DeepCopy(), rolloutBlock.DeepCopy())
		f.cycles = 1

		contender := f.buildContender(namespace, contenderName, totalReplicaCount)

		contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
		condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
		condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

		contender.release.Spec.TargetStep = 1
		contender.capacityTarget.Spec.Clusters[0].Percent = 50
		contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
		contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))
		contender.trafficTarget.Spec.Clusters[0].Weight = 50
		contender.trafficTarget.Status.Clusters[0].AchievedTraffic = 50

		f.addObjects(
			contender.release.DeepCopy(),
			contender.installationTarget.DeepCopy(),
			contender.capacityTarget.DeepCopy(),
			contender.trafficTarget.DeepCopy(),
		)

		expectedContender := contender.release.DeepCopy()
		rolloutBlockMessage := fmt.Sprintf("rollout block(s) with name(s) %s exist", rolloutBlockKey)
		condBlocked := releaseutil.NewReleaseCondition(
			shipper.ReleaseConditionTypeBlocked,
			corev1.ConditionTrue,
			"RolloutsBlocked",
			rolloutBlockMessage)
		releaseutil.SetReleaseCondition(&expectedContender.Status, *condBlocked)

		action := kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			namespace,
			expectedContender)
		f.actions = append(f.actions, action)

		f.expectedEvents = append(f.expectedEvents,
			fmt.Sprintf("%s RolloutBlocked %s", corev1.EventTypeWarning, rolloutBlockKey),
			fmt.Sprintf("Normal ReleaseConditionChanged [Blocked False] -> [Blocked True RolloutsBlocked %s]", rolloutBlockMessage))
		f.run()
	}
}

func TestContenderReleasePhaseIsWaitingForCommandForFinalStepState(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	for _, totalReplicaCount := range []int32{1, 3, 10} {
		f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())

		contender := f.buildContender(namespace, contenderName, totalReplicaCount)
		incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

		contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
		condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
		condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

		contender.release.Spec.TargetStep = 1
		contender.capacityTarget.Spec.Clusters[0].Percent = 50
		contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
		contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))
		contender.trafficTarget.Spec.Clusters[0].Weight = 50
		incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
		incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
		incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
		incumbent.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		incumbent.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))

		f.addObjects(
			contender.release.DeepCopy(),
			contender.installationTarget.DeepCopy(),
			contender.capacityTarget.DeepCopy(),
			contender.trafficTarget.DeepCopy(),

			incumbent.release.DeepCopy(),
			incumbent.installationTarget.DeepCopy(),
			incumbent.capacityTarget.DeepCopy(),
			incumbent.trafficTarget.DeepCopy(),
		)

		f.expectReleaseWaitingForCommand(contender.release, 1)
		f.run()
	}
}

func TestContenderReleaseIsInstalled(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	for _, totalReplicaCount := range []int32{1, 3, 10} {
		f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())

		contender := f.buildContender(namespace, contenderName, totalReplicaCount)
		incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

		contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
		condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
		condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
		releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

		contender.release.Spec.TargetStep = 2
		contender.capacityTarget.Spec.Clusters[0].Percent = 100
		contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
		contender.capacityTarget.Status.Clusters[0].AchievedPercent = 100
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = totalReplicaCount
		contender.trafficTarget.Spec.Clusters[0].Weight = 100

		incumbent.trafficTarget.Spec.Clusters[0].Weight = 0
		incumbent.capacityTarget.Spec.Clusters[0].Percent = 0
		incumbent.capacityTarget.Status.Clusters[0].AchievedPercent = 0
		incumbent.capacityTarget.Status.Clusters[0].AvailableReplicas = 0

		f.addObjects(
			contender.release.DeepCopy(),
			contender.installationTarget.DeepCopy(),
			contender.capacityTarget.DeepCopy(),
			contender.trafficTarget.DeepCopy(),

			incumbent.release.DeepCopy(),
			incumbent.installationTarget.DeepCopy(),
			incumbent.capacityTarget.DeepCopy(),
			incumbent.trafficTarget.DeepCopy(),
		)

		f.expectReleaseReleased(contender.release, 2)

		f.run()
	}
}

func TestApplicationExposesStrategyFailure(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")

	cluster := buildCluster("minikube")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(1)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	missingStepMsg := fmt.Sprintf("failed to execute strategy: \"no step 2 in strategy for Release \\\"%s/%s\\\"\"", contender.release.Namespace, contender.release.Name)

	contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
	condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
	condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

	// We define 2 steps and will intentionally set target step index out of this bound
	strategy := shipper.RolloutStrategy{
		Steps: []shipper.RolloutStrategyStep{
			{
				Name:     "staging",
				Capacity: shipper.RolloutStrategyStepValue{Incumbent: 100, Contender: 1},
				Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 100, Contender: 0},
			},
			{
				Name:     "full on",
				Capacity: shipper.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
				Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
			},
		},
	}

	contender.release.Spec.Environment.Strategy = &strategy
	contender.release.Spec.TargetStep = 2 // out of bound index

	expectedRel := contender.release.DeepCopy()
	expectedRel.Status.Conditions = []shipper.ReleaseCondition{
		{
			Type:   shipper.ReleaseConditionTypeBlocked,
			Status: corev1.ConditionFalse,
		},
		{
			Type:   shipper.ReleaseConditionTypeScheduled,
			Status: corev1.ConditionTrue,
		},
		{
			Type:    shipper.ReleaseConditionTypeStrategyExecuted,
			Status:  corev1.ConditionFalse,
			Reason:  conditions.StrategyExecutionFailed,
			Message: missingStepMsg,
		},
	}

	f.addObjects(
		contender.release.DeepCopy(),
		contender.installationTarget.DeepCopy(),
		contender.capacityTarget.DeepCopy(),
		contender.trafficTarget.DeepCopy(),

		incumbent.release.DeepCopy(),
		incumbent.installationTarget.DeepCopy(),
		incumbent.capacityTarget.DeepCopy(),
		incumbent.trafficTarget.DeepCopy(),
	)

	f.actions = append(f.actions, kubetesting.NewUpdateAction(
		shipper.SchemeGroupVersion.WithResource("releases"),
		namespace,
		expectedRel))

	f.filter = f.filter.Extend(actionfilter{
		[]string{"update"},
		[]string{"releases"},
	})
	f.expectedEvents = append(f.expectedEvents,
		fmt.Sprintf("Normal ReleaseConditionChanged [StrategyExecuted True] -> [StrategyExecuted False StrategyExecutionFailed %s]", missingStepMsg))

	f.run()
}

func workingOnContenderCapacity(percent int, wg *sync.WaitGroup, t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"

	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	defer wg.Done()

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
	condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
	condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

	contender.release.Spec.TargetStep = 1

	achievedCapacityPercentage := 100 - int32(percent)

	// Working on contender capacity.
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
	contender.capacityTarget.Status.Clusters[0].AchievedPercent = achievedCapacityPercentage
	contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), float64(achievedCapacityPercentage)))

	f.addObjects(
		contender.release.DeepCopy(),
		contender.installationTarget.DeepCopy(),
		contender.capacityTarget.DeepCopy(),
		contender.trafficTarget.DeepCopy(),

		incumbent.release.DeepCopy(),
		incumbent.installationTarget.DeepCopy(),
		incumbent.capacityTarget.DeepCopy(),
		incumbent.trafficTarget.DeepCopy(),
	)

	relpair := releaseInfoPair{
		contender: contender,
		incumbent: incumbent,
	}
	f.expectCapacityNotReady(relpair, 1, 0, Contender, "minikube")
	f.run()
}

func TestWaitingOnContenderTrafficProducesNoPatches(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"

	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
	condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
	condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

	contender.release.Spec.TargetStep = 1

	// Desired contender capacity achieved.
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
	contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
	contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))

	// Working on contender traffic.
	contender.trafficTarget.Spec.Clusters[0].Weight = 50
	contender.trafficTarget.Status.Conditions, _ = targetutil.SetTargetCondition(
		contender.trafficTarget.Status.Conditions,
		targetutil.NewTargetCondition(
			shipper.TargetConditionTypeReady,
			corev1.ConditionFalse,
			ClustersNotReady, "[minikube]"))

	f.addObjects(
		contender.release.DeepCopy(),
		contender.installationTarget.DeepCopy(),
		contender.capacityTarget.DeepCopy(),
		contender.trafficTarget.DeepCopy(),

		incumbent.release.DeepCopy(),
		incumbent.installationTarget.DeepCopy(),
		incumbent.capacityTarget.DeepCopy(),
		incumbent.trafficTarget.DeepCopy(),
	)

	relpair := releaseInfoPair{
		contender: contender,
		incumbent: incumbent,
	}
	f.expectTrafficNotReady(relpair, 1, 0, Contender, "minikube")
	f.run()

}

func TestWaitingOnIncumbentTrafficProducesNoPatches(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"

	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
	condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
	condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

	contender.release.Spec.TargetStep = 1

	// Desired contender capacity achieved.
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
	contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
	contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))

	// Desired contender traffic achieved.
	contender.trafficTarget.Spec.Clusters[0].Weight = 50

	// Working on incumbent traffic.
	incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
	incumbent.trafficTarget.Status.Conditions, _ = targetutil.SetTargetCondition(
		incumbent.trafficTarget.Status.Conditions,
		targetutil.NewTargetCondition(
			shipper.TargetConditionTypeReady,
			corev1.ConditionFalse,
			ClustersNotReady, "[minikube]"))

	f.addObjects(
		contender.release.DeepCopy(),
		contender.installationTarget.DeepCopy(),
		contender.capacityTarget.DeepCopy(),
		contender.trafficTarget.DeepCopy(),

		incumbent.release.DeepCopy(),
		incumbent.installationTarget.DeepCopy(),
		incumbent.capacityTarget.DeepCopy(),
		incumbent.trafficTarget.DeepCopy(),
	)

	relpair := releaseInfoPair{
		contender: contender,
		incumbent: incumbent,
	}
	f.expectTrafficNotReady(relpair, 1, 0, Incumbent, "minikube")
	f.run()
}

func workingOnIncumbentCapacity(percent int, wg *sync.WaitGroup, t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"

	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	defer wg.Done()

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	contender.release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
	condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&contender.release.Status, *condScheduled)
	condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&contender.release.Status, *condStrategyExecuted)

	contender.release.Spec.TargetStep = 1

	// Desired contender capacity achieved.
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
	contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
	contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), 50))

	// Desired contender traffic achieved.
	contender.trafficTarget.Spec.Clusters[0].Weight = 50

	// Desired incumbent traffic achieved.
	incumbent.trafficTarget.Spec.Clusters[0].Weight = 50

	// Working on incumbent capacity.
	incumbentAchievedCapacityPercentage := 100 - int32(percent)
	incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
	incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
	incumbent.capacityTarget.Status.Clusters[0].AchievedPercent = incumbentAchievedCapacityPercentage
	incumbent.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(uint(totalReplicaCount), float64(incumbentAchievedCapacityPercentage)))

	f.addObjects(
		contender.release.DeepCopy(),
		contender.installationTarget.DeepCopy(),
		contender.capacityTarget.DeepCopy(),
		contender.trafficTarget.DeepCopy(),

		incumbent.release.DeepCopy(),
		incumbent.installationTarget.DeepCopy(),
		incumbent.capacityTarget.DeepCopy(),
		incumbent.trafficTarget.DeepCopy(),
	)

	relpair := releaseInfoPair{
		contender: contender,
		incumbent: incumbent,
	}
	f.expectCapacityNotReady(relpair, 1, 0, Incumbent, "minikube")
	f.run()
}

func TestShouldNotProducePatches(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go workingOnContenderCapacity(i, &wg, t)

		wg.Add(1)
		go workingOnIncumbentCapacity(i, &wg, t)
	}
	wg.Wait()
}

func TestControllerChooseClusters(t *testing.T) {
	namespace := "test-namespace"
	app := buildApplication(namespace, "test-app")

	clusterA := buildCluster("minikube-a")
	clusterB := buildCluster("minikube-b")

	f := newFixture(t, app.DeepCopy(), clusterA.DeepCopy(), clusterB.DeepCopy())
	f.cycles = 1

	contender := buildRelease()

	replicaCount := int32(2)
	contender.Spec.Environment.ClusterRequirements.Regions[0].Replicas = &replicaCount
	f.addObjects(contender.DeepCopy())

	expected := contender.DeepCopy()
	expected.Annotations[shipper.ReleaseClustersAnnotation] = fmt.Sprintf("%s,%s", clusterA.Name, clusterB.Name)
	condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&expected.Status, *condScheduled)
	condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&expected.Status, *condStrategyExecuted)

	f.actions = []kubetesting.Action{
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			contender.GetNamespace(),
			expected),
	}

	f.filter = f.filter.Extend(actionfilter{[]string{"update"}, []string{"releases"}})

	relKey := fmt.Sprintf("%s/%s", contender.GetNamespace(), contender.GetName())
	f.expectedEvents = append(f.expectedEvents,
		fmt.Sprintf(
			"Normal ClustersSelected Set clusters for \"%s\" to %s,%s",
			relKey,
			clusterA.Name,
			clusterB.Name,
		),
		fmt.Sprintf(
			"Normal ReleaseScheduled Created InstallationTarget \"%s\"",
			relKey,
		),
		fmt.Sprintf(
			"Normal ReleaseScheduled Created TrafficTarget \"%s\"",
			relKey,
		),
		fmt.Sprintf(
			"Normal ReleaseScheduled Created CapacityTarget \"%s\"",
			relKey,
		),
		"Normal ReleaseConditionChanged [Scheduled False] -> [Scheduled True], [] -> [StrategyExecuted True]",
	)

	f.run()
}

func TestControllerChooseClustersSkipsUnschedulable(t *testing.T) {
	namespace := "test-namespace"
	app := buildApplication(namespace, "test-app")

	clusterA := buildCluster("minikube-a")
	clusterB := buildCluster("minikube-b")
	clusterB.Spec.Scheduler.Unschedulable = true

	f := newFixture(t, app.DeepCopy(), clusterA.DeepCopy(), clusterB.DeepCopy())
	f.cycles = 1

	contender := buildRelease()
	f.addObjects(contender.DeepCopy())

	expected := contender.DeepCopy()
	expected.Annotations[shipper.ReleaseClustersAnnotation] = clusterA.Name

	condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&expected.Status, *condScheduled)
	condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&expected.Status, *condStrategyExecuted)

	f.actions = []kubetesting.Action{
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			contender.GetNamespace(),
			expected),
	}

	f.filter = f.filter.Extend(actionfilter{[]string{"update"}, []string{"releases"}})

	relKey := fmt.Sprintf("%s/%s", contender.GetNamespace(), contender.GetName())
	f.expectedEvents = append(f.expectedEvents,
		fmt.Sprintf(
			"Normal ClustersSelected Set clusters for \"%s\" to %s",
			relKey,
			clusterA.Name,
		),
		fmt.Sprintf(
			"Normal ReleaseScheduled Created InstallationTarget \"%s\"",
			relKey,
		),
		fmt.Sprintf(
			"Normal ReleaseScheduled Created TrafficTarget \"%s\"",
			relKey,
		),
		fmt.Sprintf(
			"Normal ReleaseScheduled Created CapacityTarget \"%s\"",
			relKey,
		),
		"Normal ReleaseConditionChanged [Scheduled False] -> [Scheduled True], [] -> [StrategyExecuted True]",
	)

	f.run()
}
