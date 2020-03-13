package release

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
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

var fullon = shipper.RolloutStrategy{
	Steps: []shipper.RolloutStrategyStep{
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
	store           *shippertesting.FakeClusterClientStore
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
		f.store,
		f.informerFactory,
		shippertesting.LocalFetchChart,
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

	step, stepName := int32(2), "full on"

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
				Step: step,
				Name: stepName,
			},
			Conditions: []shipper.ReleaseCondition{
				{Type: shipper.ReleaseConditionTypeBlocked, Status: corev1.ConditionFalse},
				{Type: shipper.ReleaseConditionTypeComplete, Status: corev1.ConditionTrue},
				{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
				{Type: shipper.ReleaseConditionTypeStrategyExecuted, Status: corev1.ConditionTrue},
			},
			Strategy: &shipper.ReleaseStrategyStatus{
				State: shipper.ReleaseStrategyState{
					WaitingForInstallation: shipper.StrategyStateFalse,
					WaitingForTraffic:      shipper.StrategyStateFalse,
					WaitingForCapacity:     shipper.StrategyStateFalse,
					WaitingForCommand:      shipper.StrategyStateFalse,
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
				},
			},
		},
		Spec: shipper.ReleaseSpec{
			TargetStep: step,
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
			Conditions: []shipper.TargetCondition{
				{
					Type:   shipper.TargetConditionTypeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
		Spec: shipper.InstallationTargetSpec{
			Clusters: clusterNames,
		},
	}

	capacityTargetSpecClusters := make([]shipper.ClusterCapacityTarget, 0, len(clusterNames))
	for _, clusterName := range clusterNames {
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
			Conditions: []shipper.TargetCondition{
				{
					Type:   shipper.TargetConditionTypeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
		Spec: shipper.CapacityTargetSpec{
			Clusters: capacityTargetSpecClusters,
		},
	}

	trafficTargetSpecClusters := make([]shipper.ClusterTrafficTarget, 0, len(clusterNames))
	for _, clusterName := range clusterNames {
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
			Conditions: []shipper.TargetCondition{
				{
					Type:   shipper.TargetConditionTypeReady,
					Status: corev1.ConditionTrue,
				},
			},
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
				shipper.ReleaseClustersAnnotation:       strings.Join(clusterNames, ","),
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
			Conditions: []shipper.TargetCondition{
				{
					Type:   shipper.TargetConditionTypeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
		Spec: shipper.InstallationTargetSpec{
			Clusters: clusterNames,
		},
	}

	capacityTargetSpecClusters := make([]shipper.ClusterCapacityTarget, 0, len(clusterNames))
	for _, clusterName := range clusterNames {
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
			Conditions: []shipper.TargetCondition{
				{
					Type:   shipper.TargetConditionTypeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
		Spec: shipper.CapacityTargetSpec{
			Clusters: capacityTargetSpecClusters,
		},
	}

	trafficTargetSpecClusters := make([]shipper.ClusterTrafficTarget, 0, len(clusterNames))
	for _, clusterName := range clusterNames {
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
			Conditions: []shipper.TargetCondition{
				{
					Type:   shipper.TargetConditionTypeReady,
					Status: corev1.ConditionTrue,
				},
			},
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

	ri.installationTarget.Spec.Clusters = append(ri.installationTarget.Spec.Clusters,
		cluster.Name,
	)
	ri.capacityTarget.Spec.Clusters = append(ri.capacityTarget.Spec.Clusters,
		shipper.ClusterCapacityTarget{Name: cluster.Name, Percent: 0},
	)
	ri.trafficTarget.Spec.Clusters = append(ri.trafficTarget.Spec.Clusters,
		shipper.ClusterTrafficTarget{Name: cluster.Name, Weight: 0},
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
		`Normal ReleaseConditionChanged [] -> [Scheduled True], [] -> [StrategyExecuted True]`,
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
			Chart:       release.Spec.Environment.Chart,
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

func (f *fixture) expectCapacityStatusPatch(step int32, ct *shipper.CapacityTarget, r *shipper.Release, value uint, totalReplicaCount uint, role role) {
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
				State:      strategyConditions.AsReleaseStrategyState(step, true, false, true),
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

	f.expectedEvents = []string{
		"Normal ReleaseConditionChanged [] -> [Scheduled True], [] -> [StrategyExecuted True]",
	}
}

func (f *fixture) expectTrafficStatusPatch(step int32, tt *shipper.TrafficTarget, r *shipper.Release, value uint32, role role) {
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
				State:      strategyConditions.AsReleaseStrategyState(step, true, false, true),
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

	f.expectedEvents = []string{
		"Normal ReleaseConditionChanged [] -> [Scheduled True], [] -> [StrategyExecuted True]",
	}
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
		"Normal ReleaseConditionChanged [] -> [Scheduled True], [] -> [StrategyExecuted True], [] -> [Complete True]",
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

	f.expectedEvents = []string{
		fmt.Sprintf(`Normal ReleaseConditionChanged [] -> [Scheduled True], [] -> [StrategyExecuted True]`),
	}
}

func (f *fixture) expectCapacityNotReady(relpair releaseInfoPair, targetStep, achievedStepIndex int32, role role, brokenClusterName string) {
	f.filter = f.filter.Extend(actionfilter{
		[]string{"patch"},
		[]string{"releases"},
	})

	gvr := shipper.SchemeGroupVersion.WithResource("releases")

	var newStatus map[string]interface{}

	// var achievedStep *shipper.AchievedStep
	// if achievedStepIndex != 0 {
	// 	achievedStep = &shipper.AchievedStep{
	// 		Step: achievedStepIndex,
	// 		Name: rel.Spec.Environment.Strategy.Steps[achievedStepIndex].Name,
	// 	}
	// }

	rel := relpair.contender.release

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

	f.expectedEvents = []string{
		fmt.Sprintf(`Normal ReleaseConditionChanged [] -> [Scheduled True], [] -> [StrategyExecuted True]`),
	}
}

func (f *fixture) expectTrafficNotReady(relpair releaseInfoPair, targetStep, achievedStepIndex int32, role role, brokenClusterName string) {
	gvr := shipper.SchemeGroupVersion.WithResource("releases")
	var newStatus map[string]interface{}

	// var achievedStep *shipper.AchievedStep
	// if achievedStepIndex != 0 {
	// 	achievedStep = &shipper.AchievedStep{
	// 		Step: achievedStepIndex,
	// 		Name: rel.Spec.Environment.Strategy.Steps[achievedStepIndex].Name,
	// 	}
	// }

	rel := relpair.contender.release

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

func TestContenderReleasePhaseIsWaitingForCommandForInitialStepState(t *testing.T) {
	namespace := "test-namespace"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	incumbentName, contenderName := "test-incumbent", "test-contender"
	app.Status.History = []string{incumbentName, contenderName}
	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(10)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)

	contender.capacityTarget.Spec.Clusters[0].Percent = 1
	incumbent.capacityTarget.Spec.Clusters[0].Percent = 100

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
	var step int32 = 0
	f.expectReleaseWaitingForCommand(contender.release, step)
	f.run()
}

func TestContenderDoNothingClusterInstallationNotReady(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")
	brokenCluster := buildCluster("broken-installation-cluster")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	addCluster(contender, brokenCluster)

	contender.release.Spec.TargetStep = 0

	// the fixture creates installation targets in 'installation succeeded'
	// status, so we'll break one
	contender.installationTarget.Status.Conditions, _ = targetutil.SetTargetCondition(
		contender.installationTarget.Status.Conditions,
		targetutil.NewTargetCondition(
			shipper.TargetConditionTypeReady,
			corev1.ConditionFalse,
			ClustersNotReady, "[broken-installation-cluster]"))

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

func TestContenderDoNothingClusterCapacityNotReady(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")
	brokenCluster := buildCluster("broken-capacity-cluster")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	addCluster(contender, brokenCluster)

	// We'll set cluster 0 to be all set, but make cluster 1 broken.
	contender.release.Spec.TargetStep = 1
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = int32(totalReplicaCount)
	contender.trafficTarget.Spec.Clusters[0].Weight = 50

	// No capacity yet.
	contender.capacityTarget.Spec.Clusters[1].Percent = 50
	contender.capacityTarget.Spec.Clusters[1].TotalReplicaCount = totalReplicaCount
	contender.trafficTarget.Spec.Clusters[1].Weight = 50
	contender.capacityTarget.Status.Conditions, _ = targetutil.SetTargetCondition(
		contender.capacityTarget.Status.Conditions,
		targetutil.NewTargetCondition(
			shipper.TargetConditionTypeReady,
			corev1.ConditionFalse,
			ClustersNotReady, "[broken-capacity-cluster]"))

	incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
	incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
	incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount

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

func TestContenderDoNothingClusterTrafficNotReady(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")
	brokenCluster := buildCluster("broken-traffic-cluster")

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

	addCluster(contender, brokenCluster)

	// We'll set cluster 0 to be all set, but make cluster 1 broken.
	contender.release.Spec.TargetStep = 1
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
	contender.trafficTarget.Spec.Clusters[0].Weight = 50

	contender.capacityTarget.Spec.Clusters[1].Percent = 50
	contender.capacityTarget.Spec.Clusters[1].TotalReplicaCount = totalReplicaCount

	// No traffic yet.
	contender.trafficTarget.Spec.Clusters[1].Weight = 50
	contender.trafficTarget.Status.Conditions, _ = targetutil.SetTargetCondition(
		contender.trafficTarget.Status.Conditions,
		targetutil.NewTargetCondition(
			shipper.TargetConditionTypeReady,
			corev1.ConditionFalse,
			ClustersNotReady, "[broken-traffic-cluster]"))

	incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
	incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
	incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount

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

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	contender.release.Spec.TargetStep = 1

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
	f.expectCapacityStatusPatch(contender.release.Spec.TargetStep, ct, r, 50, uint(totalReplicaCount), Contender)
	f.run()
}

func TestContenderCapacityShouldIncreaseWithRolloutBlockOverride(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	rolloutBlock := newRolloutBlock(testRolloutBlockName, namespace)
	rolloutBlockKey := fmt.Sprintf("%s/%s", namespace, testRolloutBlockName)
	cluster := buildCluster("minikube")

	totalReplicaCount := int32(10)
	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy(), rolloutBlock.DeepCopy())
	f.cycles = 1

	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	contender.release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = rolloutBlockKey
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)
	incumbent.release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = rolloutBlockKey

	contender.release.Spec.TargetStep = 1

	incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
	incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
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

	ct := contender.capacityTarget.DeepCopy()
	r := contender.release.DeepCopy()
	f.expectCapacityStatusPatch(contender.release.Spec.TargetStep, ct, r, 50, uint(totalReplicaCount), Contender)
	overrideEvent := fmt.Sprintf("%s RolloutBlockOverridden %s", corev1.EventTypeNormal, rolloutBlockKey)
	f.expectedEvents = append([]string{overrideEvent}, f.expectedEvents...)
	f.run()
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

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	f.cycles = 1 // It only runs a single cycle of processNextReleaseWorkItem

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	contender.release.Spec.TargetStep = 1
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount

	incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
	incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
	incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount

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
	f.expectTrafficStatusPatch(contender.release.Spec.TargetStep, tt, r, 50, Contender)
	f.run()
}

func TestContenderTrafficShouldIncreaseWithRolloutBlockOverride(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	rolloutBlock := newRolloutBlock(testRolloutBlockName, namespace)
	rolloutBlockKey := fmt.Sprintf("%s/%s", namespace, testRolloutBlockName)
	cluster := buildCluster("minikube")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy(), rolloutBlock.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	contender.release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = rolloutBlockKey
	incumbent.release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = rolloutBlockKey

	contender.release.Spec.TargetStep = 1
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount

	incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
	incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
	incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount

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
	f.expectTrafficStatusPatch(contender.release.Spec.TargetStep, tt, r, 50, Contender)
	overrideEvent := fmt.Sprintf("%s RolloutBlockOverridden %s", corev1.EventTypeNormal, rolloutBlockKey)
	f.expectedEvents = append([]string{overrideEvent}, f.expectedEvents...)
	f.run()
}

func TestContenderTrafficShouldNotIncreaseWithRolloutBlock(t *testing.T) {
	namespace := "test-namespace"
	app := buildApplication(namespace, "test-app")
	rolloutBlock := newRolloutBlock(testRolloutBlockName, namespace)
	rolloutBlockKey := fmt.Sprintf("%s/%s", namespace, testRolloutBlockName)
	cluster := buildCluster("minikube")
	contenderName := "contender"

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy(), rolloutBlock.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)

	contender.release.Spec.TargetStep = 1
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount

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

func TestIncumbentTrafficShouldDecrease(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	contender.release.Spec.TargetStep = 1
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
	contender.trafficTarget.Spec.Clusters[0].Weight = 50
	contender.release.Status.AchievedStep = &shipper.AchievedStep{Step: 1}

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
	f.expectTrafficStatusPatch(contender.release.Spec.TargetStep, tt, r, 50, Incumbent)
	f.run()
}

func TestIncumbentTrafficShouldDecreaseWithRolloutBlockOverride(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	rolloutBlock := newRolloutBlock(testRolloutBlockName, namespace)
	rolloutBlockKey := fmt.Sprintf("%s/%s", namespace, testRolloutBlockName)
	cluster := buildCluster("minikube")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy(), rolloutBlock.DeepCopy())
	f.cycles = 1 // we're looking at a single-step progression

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	contender.release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = rolloutBlockKey
	incumbent.release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = rolloutBlockKey

	contender.release.Spec.TargetStep = 1
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
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
	f.expectTrafficStatusPatch(contender.release.Spec.TargetStep, tt, r, 50, Incumbent)
	overrideEvent := fmt.Sprintf("%s RolloutBlockOverridden %s", corev1.EventTypeNormal, rolloutBlockKey)
	f.expectedEvents = append([]string{overrideEvent}, f.expectedEvents...)
	f.run()
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

	contender.release.Spec.TargetStep = 1
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
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

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	contender.release.Spec.TargetStep = 1
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
	contender.trafficTarget.Spec.Clusters[0].Weight = 50
	contender.release.Status.AchievedStep = &shipper.AchievedStep{Step: 1}

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
	f.expectCapacityStatusPatch(contender.release.Spec.TargetStep, tt, r, 50, uint(totalReplicaCount), Incumbent)
	f.run()
}

func TestIncumbentCapacityShouldDecreaseWithRolloutBlockOverride(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	rolloutBlock := newRolloutBlock(testRolloutBlockName, namespace)
	rolloutBlockKey := fmt.Sprintf("%s/%s", namespace, testRolloutBlockName)
	cluster := buildCluster("minikube")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy(), rolloutBlock.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(3)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	contender.release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = rolloutBlockKey
	incumbent.release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = rolloutBlockKey

	contender.release.Spec.TargetStep = 1
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
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
	f.expectCapacityStatusPatch(contender.release.Spec.TargetStep, tt, r, 50, uint(totalReplicaCount), Incumbent)
	overrideEvent := fmt.Sprintf("%s RolloutBlockOverridden %s", corev1.EventTypeNormal, rolloutBlockKey)
	f.expectedEvents = append([]string{overrideEvent}, f.expectedEvents...)
	f.run()
}

func TestIncumbentCapacityShouldNotDecreaseWithRolloutBlock(t *testing.T) {
	namespace := "test-namespace"
	contenderName := "test-contender"
	app := buildApplication(namespace, "test-app")
	rolloutBlock := newRolloutBlock(testRolloutBlockName, namespace)
	rolloutBlockKey := fmt.Sprintf("%s/%s", namespace, testRolloutBlockName)
	cluster := buildCluster("minikube")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy(), rolloutBlock.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(3)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)

	contender.release.Spec.TargetStep = 1
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
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
		fmt.Sprintf("Normal ReleaseConditionChanged [Blocked False] -> [Blocked True RolloutsBlocked %s]", rolloutBlockMessage),
	)
	f.run()
}

func TestContenderReleasePhaseIsWaitingForCommandForFinalStepState(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	contender.release.Spec.TargetStep = 1
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
	contender.trafficTarget.Spec.Clusters[0].Weight = 50
	incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
	incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
	incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount

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

func TestContenderReleaseIsInstalled(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	contender.release.Spec.TargetStep = 2
	contender.capacityTarget.Spec.Clusters[0].Percent = 100
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
	contender.trafficTarget.Spec.Clusters[0].Weight = 100

	incumbent.trafficTarget.Spec.Clusters[0].Weight = 0
	incumbent.capacityTarget.Spec.Clusters[0].Percent = 0

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

func TestApplicationExposesStrategyFailureIndexOutOfBounds(t *testing.T) {
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
		fmt.Sprintf("Normal ReleaseConditionChanged [] -> [Scheduled True], [] -> [StrategyExecuted False StrategyExecutionFailed %s]", missingStepMsg))

	f.run()
}

func TestApplicationExposesStrategyFailureSuccessorIndexOutOfBounds(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")

	cluster := buildCluster("minikube")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	// we're testing 2 cycles because we expect contender and incumbent patches
	// to be issued independently
	f.cycles = 2

	totalReplicaCount := int32(1)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	// We define 2 steps and will intentionally set target step index out of this bound
	strategyStaging := shipper.RolloutStrategy{
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

	// clean up conditions as incumbent helper gives us some extras
	incumbent.release.Status.Conditions = []shipper.ReleaseCondition{
		{
			Type:   shipper.ReleaseConditionTypeBlocked,
			Status: corev1.ConditionFalse,
		},
	}

	contender.release.Spec.Environment.Strategy = &strategyStaging
	contender.release.Spec.TargetStep = 2 // out of bound index

	expectedIncumbent := incumbent.release.DeepCopy()
	expectedIncumbent.Status.Conditions = []shipper.ReleaseCondition{
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
			Message: fmt.Sprintf(`failed to execute strategy: "no step 2 in strategy for Release \"%s/%s\""`, namespace, incumbentName),
		},
	}

	expectedContender := contender.release.DeepCopy()
	expectedContender.Status.Conditions = []shipper.ReleaseCondition{
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
			Message: fmt.Sprintf(`failed to execute strategy: "no step 2 in strategy for Release \"%s/%s\""`, namespace, contenderName),
		},
	}

	// we change the order of incumbent and contender here: we want to
	// ensure we're safe when an incumbent steps in first and then triggers
	// it's successor processing.
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

	f.actions = append(f.actions,
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			namespace,
			expectedIncumbent,
		),
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			namespace,
			expectedContender,
		),
	)

	f.filter = f.filter.Extend(actionfilter{
		[]string{"update"},
		[]string{"releases"},
	})
	f.expectedEvents = append(f.expectedEvents,
		fmt.Sprintf(`Normal ReleaseConditionChanged [] -> [Scheduled True], [] -> [StrategyExecuted False StrategyExecutionFailed %s]`,
			fmt.Sprintf(`failed to execute strategy: "no step 2 in strategy for Release \"%s/%s\""`, namespace, incumbentName)),
		fmt.Sprintf(`Normal ReleaseConditionChanged [] -> [Scheduled True], [] -> [StrategyExecuted False StrategyExecutionFailed %s]`,
			fmt.Sprintf(`failed to execute strategy: "no step 2 in strategy for Release \"%s/%s\""`, namespace, contenderName)),
	)

	f.run()
}

func TestWaitingOnContenderCapacityProducesNoPatches(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"

	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	contender.release.Spec.TargetStep = 1

	// Working on contender capacity.
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
	contender.capacityTarget.Status.Conditions, _ = targetutil.SetTargetCondition(
		contender.capacityTarget.Status.Conditions,
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

func TestWaitingOnIncumbentCapacityProducesNoPatches(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"

	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	contender.release.Spec.TargetStep = 1

	// Desired contender capacity achieved.
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount

	// Desired contender traffic achieved.
	contender.trafficTarget.Spec.Clusters[0].Weight = 50

	// Desired incumbent traffic achieved.
	incumbent.trafficTarget.Spec.Clusters[0].Weight = 50

	// Working on incumbent capacity.
	incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
	incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
	incumbent.capacityTarget.Status.Conditions, _ = targetutil.SetTargetCondition(
		incumbent.capacityTarget.Status.Conditions,
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
	f.expectCapacityNotReady(relpair, 1, 0, Incumbent, "minikube")
	f.run()
}

func TestIncumbentOutOfRangeTargetStep(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	f.cycles = 2

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	// Incumbent spec contains only 1 strategy step but we intentionally
	// specify an out-of-range index in order to test if it carefully
	// handles indices.
	incumbent.release.Spec.TargetStep = 2
	incumbent.release.Spec.Environment.Strategy = &fullon
	incumbent.release.Status.AchievedStep.Step = 0
	incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
	incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
	incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount

	step := int32(1)
	contender.release.Spec.TargetStep = step
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = totalReplicaCount
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

	f.expectReleaseWaitingForCommand(contender.release, step)
	f.expectedEvents = append(f.expectedEvents,
		fmt.Sprintf(`Normal ReleaseConditionChanged [StrategyExecuted True] -> [StrategyExecuted False StrategyExecutionFailed failed to execute strategy: "Release %s/%s target step is inconsistent: unexpected value %d (expected: 0)"]`,
			namespace, incumbentName, 2))

	f.run()
}

func TestUnhealthyTrafficAndCapacityIncumbentConvergesConsistently(t *testing.T) {
	namespace := "test-namespace"
	incumbentName, contenderName := "test-incumbent", "test-contender"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")
	brokenCluster := buildCluster("broken-cluster")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	replicaCount := int32(4)

	contender := f.buildContender(namespace, contenderName, replicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, replicaCount)

	addCluster(incumbent, brokenCluster)

	// Mark contender as fully healthy
	var step int32 = 2
	contender.release.Spec.TargetStep = step
	contender.release.Status.AchievedStep = &shipper.AchievedStep{Step: 2}
	contender.release.Status = shipper.ReleaseStatus{
		Conditions: []shipper.ReleaseCondition{
			{Type: shipper.ReleaseConditionTypeBlocked, Status: corev1.ConditionFalse},
			{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
		},
		Strategy: &shipper.ReleaseStrategyStatus{
			State: shipper.ReleaseStrategyState{
				WaitingForInstallation: shipper.StrategyStateFalse,
				WaitingForCommand:      shipper.StrategyStateFalse,
				WaitingForTraffic:      shipper.StrategyStateFalse,
				WaitingForCapacity:     shipper.StrategyStateFalse,
			},
			Conditions: []shipper.ReleaseStrategyCondition{
				shipper.ReleaseStrategyCondition{
					Type:   shipper.StrategyConditionContenderAchievedCapacity,
					Status: corev1.ConditionTrue,
					Step:   step,
				},
				shipper.ReleaseStrategyCondition{
					Type:   shipper.StrategyConditionContenderAchievedInstallation,
					Status: corev1.ConditionTrue,
					Step:   step,
				},
				shipper.ReleaseStrategyCondition{
					Type:   shipper.StrategyConditionContenderAchievedTraffic,
					Status: corev1.ConditionTrue,
					Step:   step,
				},
			},
		},
	}

	contender.capacityTarget.Spec.Clusters[0].Percent = 100
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = replicaCount
	contender.trafficTarget.Spec.Clusters[0].Weight = 100

	incumbent.trafficTarget.Spec.Clusters[0].Weight = 0
	incumbent.trafficTarget.Spec.Clusters[1].Weight = 0
	incumbent.trafficTarget.Status.Clusters = []*shipper.ClusterTrafficStatus{
		{
			AchievedTraffic: 50,
		},
		{
			AchievedTraffic: 0,
		},
	}
	incumbent.trafficTarget.Status.Conditions, _ = targetutil.SetTargetCondition(
		incumbent.trafficTarget.Status.Conditions,
		targetutil.NewTargetCondition(
			shipper.TargetConditionTypeReady,
			corev1.ConditionFalse,
			ClustersNotReady, "[broken-cluster]",
		),
	)
	incumbent.capacityTarget.Spec.Clusters[0].Name = "broken-cluster"
	incumbent.capacityTarget.Spec.Clusters[0].Percent = 0
	incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = replicaCount
	incumbent.capacityTarget.Spec.Clusters[1].Name = "minikube"
	incumbent.capacityTarget.Spec.Clusters[1].Percent = 0
	incumbent.capacityTarget.Spec.Clusters[1].TotalReplicaCount = 0
	incumbent.capacityTarget.Status.Conditions, _ = targetutil.SetTargetCondition(
		incumbent.capacityTarget.Status.Conditions,
		targetutil.NewTargetCondition(
			shipper.TargetConditionTypeReady,
			corev1.ConditionFalse,
			ClustersNotReady, "[broken-cluster]"))
	incumbent.capacityTarget.Status.Clusters = []shipper.ClusterCapacityStatus{
		{
			AvailableReplicas: 42, // anything but spec-matching
			AchievedPercent:   42, // anything but spec-matching
		},
		{
			AvailableReplicas: 0,
			AchievedPercent:   0,
		},
	}

	expected := contender.release.DeepCopy()
	condScheduled := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&expected.Status, *condScheduled)
	condStrategyExecuted := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeStrategyExecuted, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&expected.Status, *condStrategyExecuted)

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

	f.actions = append(f.actions,
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			contender.release.GetNamespace(),
			expected))

	var patch []byte

	newContenderStatus := map[string]interface{}{
		"status": shipper.ReleaseStatus{
			Strategy: &shipper.ReleaseStrategyStatus{
				State: shipper.ReleaseStrategyState{
					WaitingForInstallation: shipper.StrategyStateFalse,
					WaitingForCommand:      shipper.StrategyStateFalse,
					WaitingForTraffic:      shipper.StrategyStateTrue,
					WaitingForCapacity:     shipper.StrategyStateFalse,
				},
				Conditions: []shipper.ReleaseStrategyCondition{
					shipper.ReleaseStrategyCondition{
						Type:   shipper.StrategyConditionContenderAchievedCapacity,
						Status: corev1.ConditionTrue,
						Step:   step,
					},
					shipper.ReleaseStrategyCondition{
						Type:   shipper.StrategyConditionContenderAchievedInstallation,
						Status: corev1.ConditionTrue,
						Step:   step,
					},
					shipper.ReleaseStrategyCondition{
						Type:   shipper.StrategyConditionContenderAchievedTraffic,
						Status: corev1.ConditionTrue,
						Step:   step,
					},
					shipper.ReleaseStrategyCondition{
						Type:    shipper.StrategyConditionIncumbentAchievedTraffic,
						Status:  corev1.ConditionFalse,
						Step:    step,
						Reason:  ClustersNotReady,
						Message: fmt.Sprintf("release \"test-incumbent\" hasn't achieved traffic in clusters: [broken-cluster]. for more details try `kubectl describe tt test-incumbent`"),
					},
				},
			},
		},
	}
	patch, _ = json.Marshal(newContenderStatus)

	f.actions = append(f.actions, kubetesting.NewPatchAction(
		shipper.SchemeGroupVersion.WithResource("releases"),
		contender.release.GetNamespace(),
		contender.release.GetName(),
		types.MergePatchType,
		patch,
	))

	f.expectedEvents = append(f.expectedEvents,
		`Normal ReleaseConditionChanged [] -> [StrategyExecuted True]`)

	f.run()
}

func TestControllerDetectInconsistentTargetStep(t *testing.T) {
	namespace := "test-namespace"
	app := buildApplication(namespace, "test-app")
	incumbentName, contenderName := "test-incumbent", "test-contender"

	cluster := buildCluster("minikube")

	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
	f.cycles = 1

	totalReplicaCount := int32(10)
	contender := f.buildContender(namespace, contenderName, totalReplicaCount)
	incumbent := f.buildIncumbent(namespace, incumbentName, totalReplicaCount)

	contender.release.Spec.TargetStep = 2
	incumbent.release.Spec.TargetStep = 1

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

	expected := incumbent.release.DeepCopy()
	condStrategyExecuted := releaseutil.NewReleaseCondition(
		shipper.ReleaseConditionTypeStrategyExecuted,
		corev1.ConditionFalse,
		"StrategyExecutionFailed",
		"failed to execute strategy: \"Release test-namespace/test-incumbent target step is inconsistent: unexpected value 1 (expected: 2)\"",
	)
	releaseutil.SetReleaseCondition(&expected.Status, *condStrategyExecuted)

	f.actions = []kubetesting.Action{
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			incumbent.release.GetNamespace(),
			expected),
	}

	f.expectedEvents = append(f.expectedEvents,
		fmt.Sprintf("Normal ReleaseConditionChanged [StrategyExecuted True] -> [StrategyExecuted False StrategyExecutionFailed failed to execute strategy: \"Release test-namespace/test-incumbent target step is inconsistent: unexpected value 1 (expected: 2)\"]"),
	)

	f.run()
}

// This test ensures we review historical release strategy conditions
func TestUpdatesHistoricalReleaseStrategyStateConditions(t *testing.T) {
	namespace := "test-namespace"
	app := buildApplication(namespace, "test-app")
	totalReplicaCount := int32(4)

	cluster := buildCluster("minikube")
	f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())

	step := int32(2)

	// we are not interested in all updates happening in the loop and we expect
	// the first one to update the very first release in the chain.
	f.cycles = 1

	// we instantiate 3 releases: in this case the first one will be left out of
	// the "extended" (contender-incumbent) strategy executor loop and reviewed
	// with a reduced amount of ensurer steps
	relinfos := []*releaseInfo{
		f.buildIncumbent(namespace, "pre-incumbent", totalReplicaCount),
		f.buildIncumbent(namespace, "incumbent", totalReplicaCount),
		// we create a full-on release intentionally, therefore we use
		// buildIncumbent helper
		f.buildContender(namespace, "contender", totalReplicaCount),
	}

	// make sure generation-sorted releases preserve the original order
	for i, relinfo := range relinfos {
		relinfo.release.ObjectMeta.Annotations[shipper.ReleaseGenerationAnnotation] = strconv.Itoa(i)
	}

	relinfos[0].capacityTarget.Spec.Clusters = []shipper.ClusterCapacityTarget{
		{
			Name:              cluster.Name,
			Percent:           0,
			TotalReplicaCount: totalReplicaCount,
		},
	}
	relinfos[0].trafficTarget.Spec.Clusters = []shipper.ClusterTrafficTarget{
		{
			Name:   cluster.Name,
			Weight: 0,
		},
	}

	relinfos[2].capacityTarget.Spec.Clusters = []shipper.ClusterCapacityTarget{
		{
			Name:              cluster.Name,
			Percent:           1,
			TotalReplicaCount: totalReplicaCount,
		},
	}

	// we intenitonally set one of the strategy conditions to an unready state
	// and expect it to get fixed by the controller
	preincumbent := relinfos[0].release
	cond := conditions.NewStrategyConditions(preincumbent.Status.Strategy.Conditions...)
	cond.SetFalse(
		shipper.StrategyConditionContenderAchievedCapacity,
		conditions.StrategyConditionsUpdate{
			Reason: ClustersNotReady,
			// this message is incomplete but it doesn't matter in the context
			// of this test
			Message: fmt.Sprintf("release %q hasn't achieved capacity in clusters: %s",
				preincumbent.Name, cluster.Name),
			Step:               step,
			LastTransitionTime: time.Now(),
		},
	)
	preincumbent.Status.Strategy = &shipper.ReleaseStrategyStatus{
		Conditions: cond.AsReleaseStrategyConditions(),
		State:      cond.AsReleaseStrategyState(step, false, true, false),
	}

	for _, relinfo := range relinfos {
		f.addObjects(
			relinfo.release.DeepCopy(),
			relinfo.installationTarget.DeepCopy(),
			relinfo.capacityTarget.DeepCopy(),
			relinfo.trafficTarget.DeepCopy(),
		)
	}

	expectedStatus := map[string]interface{}{
		"status": shipper.ReleaseStatus{
			Strategy: &shipper.ReleaseStrategyStatus{
				State: shipper.ReleaseStrategyState{
					WaitingForInstallation: shipper.StrategyStateFalse,
					WaitingForCommand:      shipper.StrategyStateFalse,
					WaitingForTraffic:      shipper.StrategyStateFalse,
					WaitingForCapacity:     shipper.StrategyStateFalse,
				},
				Conditions: []shipper.ReleaseStrategyCondition{
					shipper.ReleaseStrategyCondition{
						Type:   shipper.StrategyConditionContenderAchievedCapacity,
						Status: corev1.ConditionTrue,
						Step:   step,
					},
					shipper.ReleaseStrategyCondition{
						Type:   shipper.StrategyConditionContenderAchievedInstallation,
						Status: corev1.ConditionTrue,
						Step:   step,
					},
					shipper.ReleaseStrategyCondition{
						Type:   shipper.StrategyConditionContenderAchievedTraffic,
						Status: corev1.ConditionTrue,
						Step:   step,
					},
				},
			},
		},
	}
	patch, _ := json.Marshal(expectedStatus)
	f.actions = append(f.actions, kubetesting.NewPatchAction(
		shipper.SchemeGroupVersion.WithResource("releases"),
		preincumbent.GetNamespace(),
		preincumbent.GetName(),
		types.MergePatchType,
		patch,
	))

	key := fmt.Sprintf("%s/%s", preincumbent.GetNamespace(), preincumbent.GetName())

	f.expectedEvents = append(f.expectedEvents,
		fmt.Sprintf(
			"Normal ReleaseStateTransitioned Release %q had its state \"WaitingForCapacity\" transitioned to \"False\"",
			key))

	f.run()
}
