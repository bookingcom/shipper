package release

import (
	"encoding/json"
	"fmt"
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

func TestControllerComputeTargetClusters(t *testing.T) {
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	fixtures := []runtime.Object{cluster, release}

	f := newFixture(t)

	// Expected values. The release should have, at the end of the business logic,
	// a list of clusters containing the sole cluster we've added to the client.
	expected := release.DeepCopy()
	expected.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()

	relWithConditions := expected.DeepCopy()
	condition := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&relWithConditions.Status, *condition)

	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expected),
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			relWithConditions),
	}

	//c, clientset := newController(fixtures...)
	c, i := f.newReleaseController()
	c.processNextReleaseWorkItem()

	filteredActions := filterActions(clientset.Actions(), []string{"update"}, []string{"releases"})
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// interestingReplicaCounts represents number of replicas that we've seen
// Shipper misbehave because of wrong replica counts based on the percentages
// the strategy specifies.
var interestingReplicaCounts = []uint{1, 3, 10}

func TestContenderReleasePhaseIsWaitingForCommandForInitialStepState(t *testing.T) {
	for _, i := range interestingReplicaCounts {

		f := newFixture(t)

		totalReplicaCount := i
		contender := buildContender(totalReplicaCount)
		incumbent := buildIncumbent(totalReplicaCount)

		// Strategy specifies that step 0 the contender has a minimum number of pods
		// (1), no traffic yet.
		contender.capacityTarget.Spec.Clusters[0].Percent = 1
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = 1
		incumbent.capacityTarget.Spec.Clusters[0].Percent = 100
		incumbent.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(totalReplicaCount)

		f.addObjects(contender, incumbent)

		fmt.Printf("f: %#v\n", f)

		rel := contender.release.DeepCopy()
		f.expectReleaseWaitingForCommand(rel, 0)
		f.run()
	}
}

type fixture struct {
	t                     *testing.T
	client                *shipperfake.Clientset
	discovery             *fakediscovery.FakeDiscovery
	clientPool            *fakedynamic.FakeClientPool
	actions               []kubetesting.Action
	objects               []runtime.Object
	receivedEvents        []string
	expectedOrderedEvents []string
	recorder              *record.FakeRecorder
}

func newFixture(t *testing.T) *fixture {
	return &fixture{t: t, receivedEvents: []string{}}
}

func (f *fixture) addObjects(releaseInfos ...*releaseInfo) {
	for _, r := range releaseInfos {
		f.objects = append(f.objects,
			r.release.DeepCopy(),
			r.capacityTarget.DeepCopy(),
			r.installationTarget.DeepCopy(),
			r.trafficTarget.DeepCopy())
	}
}

func (f *fixture) newReleaseController() (*ReleaseController, shipperinformers.SharedInformerFactory) {

	f.objects = append(f.objects, app)
	f.client = shipperfake.NewSimpleClientset(f.objects...)

	fakeDiscovery, _ := f.client.Discovery().(*fakediscovery.FakeDiscovery)
	f.discovery = fakeDiscovery
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
					Kind:       "Cluster",
					Namespaced: true,
					Name:       "clusters",
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
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(f.client, syncPeriod)

	f.clientPool = &fakedynamic.FakeClientPool{}

	f.recorder = record.NewFakeRecorder(42)

	c := NewReleaseController(f.client, shipperInformerFactory, chart.FetchRemote(), f.clientPool, f.recorder)

	return c, shipperInformerFactory
}

func (f *fixture) run() {
	c, i := f.newReleaseController()

	stopCh := make(chan struct{})
	defer close(stopCh)

	fmt.Println("starting the fixture")
	i.Start(stopCh)
	fmt.Println("waiting for cache to complete the sync")
	i.WaitForCacheSync(stopCh)
	fmt.Println("done waiting")

	wait.PollUntil(
		10*time.Millisecond,
		func() (bool, error) { return c.relQueue.Len() >= 1, nil },
		stopCh,
	)

	fmt.Println("done polling")

	readyCh := make(chan struct{})
	go func() {
		for e := range f.recorder.Events {
			fmt.Printf("received an event: %#v\n", e)
			f.receivedEvents = append(f.receivedEvents, e)
		}
		close(readyCh)
	}()

	fmt.Println("processing next release item")
	c.processNextReleaseWorkItem()

	fmt.Println("processing next app item")
	c.processNextAppWorkItem()

	fmt.Println("waiting for recorder to close")

	close(f.recorder.Events)
	<-readyCh

	actual := shippertesting.FilterActions(f.clientPool.Actions())
	shippertesting.CheckActions(f.actions, actual, f.t)
	shippertesting.CheckEvents(f.expectedOrderedEvents, f.receivedEvents, f.t)
}

func (f *fixture) expectCapacityStatusPatch(ct *shipper.CapacityTarget, r *shipper.Release, value uint, totalReplicaCount uint, role role) {
	gvr := shipper.SchemeGroupVersion.WithResource("capacitytargets")
	newSpec := map[string]interface{}{
		"spec": shipper.CapacityTargetSpec{
			Clusters: []shipper.ClusterCapacityTarget{
				{Name: "minikube", Percent: int32(value), TotalReplicaCount: int32(totalReplicaCount)},
			},
		},
	}
	patch, _ := json.Marshal(newSpec)
	action := kubetesting.NewPatchAction(gvr, ct.GetNamespace(), ct.GetName(), patch)
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
				Type:    shipper.StrategyConditionContenderAchievedCapacity,
				Status:  corev1.ConditionFalse,
				Step:    step,
				Reason:  conditions.ClustersNotReady,
				Message: "clusters pending capacity adjustments: [minikube]",
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
				Type:    shipper.StrategyConditionIncumbentAchievedCapacity,
				Status:  corev1.ConditionFalse,
				Step:    step,
				Reason:  conditions.ClustersNotReady,
				Message: "clusters pending capacity adjustments: [minikube]",
			},
		)
	}

	r.Status.Strategy = &shipper.ReleaseStrategyStatus{
		Conditions: strategyConditions.AsReleaseStrategyConditions(),
		State:      strategyConditions.AsReleaseStrategyState(r.Spec.TargetStep, true, false),
	}
	newStatus := map[string]interface{}{
		"status": r.Status,
	}
	patch, _ = json.Marshal(newStatus)
	action = kubetesting.NewPatchAction(
		shipper.SchemeGroupVersion.WithResource("releases"),
		r.GetNamespace(),
		r.GetName(),
		patch)
	f.actions = append(f.actions, action)

	f.expectedOrderedEvents = []string{}
}

func (f *fixture) expectTrafficStatusPatch(tt *shipper.TrafficTarget, r *shipper.Release, value uint32, role role) {
	gvr := shipper.SchemeGroupVersion.WithResource("traffictargets")
	newSpec := map[string]interface{}{
		"spec": shipper.TrafficTargetSpec{
			Clusters: []shipper.ClusterTrafficTarget{
				{Name: "minikube", Weight: value},
			},
		},
	}
	patch, _ := json.Marshal(newSpec)
	action := kubetesting.NewPatchAction(gvr, tt.GetNamespace(), tt.GetName(), patch)
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
				Type:    shipper.StrategyConditionContenderAchievedTraffic,
				Status:  corev1.ConditionFalse,
				Step:    step,
				Reason:  conditions.ClustersNotReady,
				Message: "clusters pending traffic adjustments: [minikube]",
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
				Type:    shipper.StrategyConditionIncumbentAchievedTraffic,
				Status:  corev1.ConditionFalse,
				Step:    step,
				Reason:  conditions.ClustersNotReady,
				Message: "clusters pending traffic adjustments: [minikube]",
			},
		)
	}

	r.Status.Strategy = &shipper.ReleaseStrategyStatus{
		Conditions: strategyConditions.AsReleaseStrategyConditions(),
		State:      strategyConditions.AsReleaseStrategyState(r.Spec.TargetStep, true, false),
	}
	newStatus := map[string]interface{}{
		"status": r.Status,
	}
	patch, _ = json.Marshal(newStatus)
	action = kubetesting.NewPatchAction(
		shipper.SchemeGroupVersion.WithResource("releases"),
		r.GetNamespace(),
		r.GetName(),
		patch)
	f.actions = append(f.actions, action)

	f.expectedOrderedEvents = []string{}
}

func (f *fixture) expectReleaseReleased(rel *shipper.Release, targetStep int32) {
	gvr := shipper.SchemeGroupVersion.WithResource("releases")
	newStatus := map[string]interface{}{
		"status": shipper.ReleaseStatus{
			AchievedStep: &shipper.AchievedStep{
				Step: targetStep,
				Name: rel.Spec.Environment.Strategy.Steps[targetStep].Name,
			},
			Conditions: []shipper.ReleaseCondition{
				{Type: shipper.ReleaseConditionTypeComplete, Status: corev1.ConditionTrue},
				{Type: shipper.ReleaseConditionTypeInstalled, Status: corev1.ConditionTrue},
				{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
			},
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
	action := kubetesting.NewPatchAction(gvr, rel.GetNamespace(), rel.GetName(), patch)

	f.actions = append(f.actions, action)

	f.expectedOrderedEvents = []string{
		fmt.Sprintf("Normal StrategyApplied step [%d] finished", targetStep),
		`Normal ReleaseStateTransitioned Release "test-namespace/0.0.2" had its state "WaitingForCapacity" transitioned to "False"`,
		`Normal ReleaseStateTransitioned Release "test-namespace/0.0.2" had its state "WaitingForCommand" transitioned to "False"`,
		`Normal ReleaseStateTransitioned Release "test-namespace/0.0.2" had its state "WaitingForInstallation" transitioned to "False"`,
		`Normal ReleaseStateTransitioned Release "test-namespace/0.0.2" had its state "WaitingForTraffic" transitioned to "False"`,
	}
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

	f.expectedOrderedEvents = []string{
		fmt.Sprintf("Normal StrategyApplied step [%d] finished", step),
		`Normal ReleaseStateTransitioned Release "test-namespace/0.0.2" had its state "WaitingForCapacity" transitioned to "False"`,
		`Normal ReleaseStateTransitioned Release "test-namespace/0.0.2" had its state "WaitingForCommand" transitioned to "True"`,
		`Normal ReleaseStateTransitioned Release "test-namespace/0.0.2" had its state "WaitingForInstallation" transitioned to "False"`,
		`Normal ReleaseStateTransitioned Release "test-namespace/0.0.2" had its state "WaitingForTraffic" transitioned to "False"`,
	}
}

// NOTE(btyler): when we add tests to use this function with a wider set of use
// cases, we'll need a "pint32(int32) *int32" func to let us take pointers to literals
func (f *fixture) expectInstallationNotReady(rel *shipper.Release, achievedStepIndex *int32, targetStepIndex int32, role role) {
	gvr := shipper.SchemeGroupVersion.WithResource("releases")

	var achievedStep *shipper.AchievedStep
	if achievedStepIndex != nil {
		achievedStep = &shipper.AchievedStep{
			Step: *achievedStepIndex,
			Name: rel.Spec.Environment.Strategy.Steps[*achievedStepIndex].Name,
		}
	}

	newStatus := map[string]interface{}{
		"status": shipper.ReleaseStatus{
			AchievedStep: achievedStep,
			Conditions: []shipper.ReleaseCondition{
				{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
			},
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
						Reason:  conditions.ClustersNotReady,
						Step:    targetStepIndex,
						Message: "clusters pending installation: [broken-installation-cluster]",
					},
				},
			},
		},
	}

	patch, _ := json.Marshal(newStatus)
	action := kubetesting.NewPatchAction(gvr, rel.GetNamespace(), rel.GetName(), patch)

	f.actions = append(f.actions, action)

	f.expectedOrderedEvents = []string{}
}

func (f *fixture) expectCapacityNotReady(rel *shipper.Release, targetStep, achievedStepIndex int32, role role, brokenClusterName string) {
	gvr := shipper.SchemeGroupVersion.WithResource("releases")

	var newStatus map[string]interface{}

	var achievedStep *shipper.AchievedStep
	if achievedStepIndex != 0 {
		achievedStep = &shipper.AchievedStep{
			Step: achievedStepIndex,
			Name: rel.Spec.Environment.Strategy.Steps[achievedStepIndex].Name,
		}
	}

	if role == Contender {
		newStatus = map[string]interface{}{
			"status": shipper.ReleaseStatus{
				AchievedStep: achievedStep,
				Conditions: []shipper.ReleaseCondition{
					{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
				},
				Strategy: &shipper.ReleaseStrategyStatus{
					State: shipper.ReleaseStrategyState{
						WaitingForInstallation: shipper.StrategyStateFalse,
						WaitingForCommand:      shipper.StrategyStateFalse,
						WaitingForTraffic:      shipper.StrategyStateFalse,
						WaitingForCapacity:     shipper.StrategyStateTrue,
					},
					Conditions: []shipper.ReleaseStrategyCondition{
						{
							Type:    shipper.StrategyConditionContenderAchievedCapacity,
							Status:  corev1.ConditionFalse,
							Reason:  conditions.ClustersNotReady,
							Message: fmt.Sprintf("clusters pending capacity adjustments: [%s]", brokenClusterName),
							Step:    targetStep,
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
				AchievedStep: achievedStep,
				Conditions: []shipper.ReleaseCondition{
					{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
				},
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
							Type:    shipper.StrategyConditionIncumbentAchievedCapacity,
							Status:  corev1.ConditionFalse,
							Reason:  conditions.ClustersNotReady,
							Step:    targetStep,
							Message: fmt.Sprintf("clusters pending capacity adjustments: [%s]", brokenClusterName),
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
	action := kubetesting.NewPatchAction(gvr, rel.GetNamespace(), rel.GetName(), patch)

	f.actions = append(f.actions, action)

	f.expectedOrderedEvents = []string{}
}

func (f *fixture) expectTrafficNotReady(rel *shipper.Release, targetStep, achievedStepIndex int32, role role, brokenClusterName string) {
	gvr := shipper.SchemeGroupVersion.WithResource("releases")
	var newStatus map[string]interface{}

	var achievedStep *shipper.AchievedStep
	if achievedStepIndex != 0 {
		achievedStep = &shipper.AchievedStep{
			Step: achievedStepIndex,
			Name: rel.Spec.Environment.Strategy.Steps[achievedStepIndex].Name,
		}
	}

	if role == Contender {
		newStatus = map[string]interface{}{
			"status": shipper.ReleaseStatus{
				AchievedStep: achievedStep,
				Conditions: []shipper.ReleaseCondition{
					{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
				},
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
							Type:    shipper.StrategyConditionContenderAchievedTraffic,
							Status:  corev1.ConditionFalse,
							Reason:  conditions.ClustersNotReady,
							Message: fmt.Sprintf("clusters pending traffic adjustments: [%s]", brokenClusterName),
							Step:    targetStep,
						},
					},
				},
			},
		}
	} else {
		newStatus = map[string]interface{}{
			"status": shipper.ReleaseStatus{
				AchievedStep: achievedStep,
				Conditions: []shipper.ReleaseCondition{
					{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
				},
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
							Type:    shipper.StrategyConditionIncumbentAchievedTraffic,
							Status:  corev1.ConditionFalse,
							Reason:  conditions.ClustersNotReady,
							Message: fmt.Sprintf("clusters pending traffic adjustments: [%s]", brokenClusterName),
							Step:    targetStep,
						},
					},
				},
			},
		}
	}

	patch, _ := json.Marshal(newStatus)
	action := kubetesting.NewPatchAction(gvr, rel.GetNamespace(), rel.GetName(), patch)

	f.actions = append(f.actions, action)

	f.expectedOrderedEvents = []string{}
}

func buildRelease() *shipper.Release {
	return &shipper.Release{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "Release",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-release",
			Namespace:   shippertesting.TestNamespace,
			Annotations: map[string]string{},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Kind:       "Application",
					Name:       "test-application",
				},
			},
		},
		Spec: shipper.ReleaseSpec{
			Environment: shipper.ReleaseEnvironment{
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
