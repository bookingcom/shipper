package strategy

import (
	"encoding/json"
	"fmt"
	"sync"
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
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/conditions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
	"github.com/bookingcom/shipper/pkg/util/replicas"
)

func init() {
	releaseutil.ConditionsShouldDiscardTimestamps = true
	conditions.StrategyConditionsShouldDiscardTimestamps = true
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

		rel := contender.release.DeepCopy()
		f.expectReleaseWaitingForCommand(rel, 0)
		f.run()

	}
}

func TestContenderDoNothingClusterInstallationNotReady(t *testing.T) {
	for _, i := range interestingReplicaCounts {
		f := newFixture(t)

		totalReplicaCount := i
		contender := buildContender(totalReplicaCount)
		incumbent := buildIncumbent(totalReplicaCount)

		addCluster(contender, "broken-installation-cluster")

		contender.release.Spec.TargetStep = 0

		// the fixture creates installation targets in 'installation succeeded' status,
		// so we'll break one
		contender.installationTarget.Status.Clusters[1].Status = shipper.InstallationStatusFailed

		f.addObjects(contender, incumbent)

		r := contender.release.DeepCopy()
		f.expectInstallationNotReady(r, nil, 0, Contender)
		f.run()
	}
}

func TestContenderDoNothingClusterCapacityNotReady(t *testing.T) {
	for _, i := range interestingReplicaCounts {
		f := newFixture(t)

		totalReplicaCount := i
		contender := buildContender(totalReplicaCount)
		incumbent := buildIncumbent(totalReplicaCount)

		brokenClusterName := "broken-capacity-cluster"
		addCluster(contender, brokenClusterName)

		// We'll set cluster 0 to be all set, but make cluster 1 broken.
		contender.release.Spec.TargetStep = 1
		contender.capacityTarget.Spec.Clusters[0].Percent = 50
		contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = int32(totalReplicaCount)
		contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(totalReplicaCount, 50))
		contender.trafficTarget.Spec.Clusters[0].Weight = 50
		contender.trafficTarget.Status.Clusters[0].AchievedTraffic = 50

		// No capacity yet.
		contender.capacityTarget.Spec.Clusters[1].Percent = 50
		contender.capacityTarget.Spec.Clusters[1].TotalReplicaCount = int32(totalReplicaCount)
		contender.capacityTarget.Status.Clusters[1].AchievedPercent = 0
		contender.capacityTarget.Status.Clusters[1].AvailableReplicas = 0
		contender.trafficTarget.Spec.Clusters[1].Weight = 50
		contender.trafficTarget.Status.Clusters[1].AchievedTraffic = 50

		incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
		incumbent.trafficTarget.Status.Clusters[0].AchievedTraffic = 50
		incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
		incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = int32(totalReplicaCount)
		incumbent.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		incumbent.capacityTarget.Status.Clusters[0].AchievedPercent = int32(replicas.CalculateDesiredReplicaCount(totalReplicaCount, 50))

		f.addObjects(contender, incumbent)

		r := contender.release.DeepCopy()
		f.expectCapacityNotReady(r, 1, 0, Contender, brokenClusterName)
		f.run()
	}
}

func TestContenderDoNothingClusterTrafficNotReady(t *testing.T) {
	for _, i := range interestingReplicaCounts {
		f := newFixture(t)

		totalReplicaCount := i
		contender := buildContender(totalReplicaCount)
		incumbent := buildIncumbent(totalReplicaCount)

		brokenClusterName := "broken-traffic-cluster"
		addCluster(contender, brokenClusterName)

		// We'll set cluster 0 to be all set, but make cluster 1 broken.
		contender.release.Spec.TargetStep = 1
		contender.capacityTarget.Spec.Clusters[0].Percent = 50
		contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = int32(totalReplicaCount)
		contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(totalReplicaCount, 50))
		contender.trafficTarget.Spec.Clusters[0].Weight = 50
		contender.trafficTarget.Status.Clusters[0].AchievedTraffic = 50

		contender.capacityTarget.Spec.Clusters[1].Percent = 50
		contender.capacityTarget.Spec.Clusters[1].TotalReplicaCount = int32(totalReplicaCount)
		contender.capacityTarget.Status.Clusters[1].AchievedPercent = 50
		contender.capacityTarget.Status.Clusters[1].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(totalReplicaCount, 50))

		contender.trafficTarget.Spec.Clusters[1].Weight = 50
		// No traffic yet.
		contender.trafficTarget.Status.Clusters[1].AchievedTraffic = 0

		incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
		incumbent.trafficTarget.Status.Clusters[0].AchievedTraffic = 50
		incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
		incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = int32(totalReplicaCount)
		incumbent.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		incumbent.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(totalReplicaCount, 50))

		f.addObjects(contender, incumbent)

		r := contender.release.DeepCopy()
		f.expectTrafficNotReady(r, 1, 0, Contender, brokenClusterName)
		f.run()
	}
}

func TestContenderCapacityShouldIncrease(t *testing.T) {
	for _, i := range interestingReplicaCounts {
		f := newFixture(t)

		totalReplicaCount := i
		contender := buildContender(totalReplicaCount)
		incumbent := buildIncumbent(totalReplicaCount)

		contender.release.Spec.TargetStep = 1

		f.addObjects(contender, incumbent)

		ct := contender.capacityTarget.DeepCopy()
		r := contender.release.DeepCopy()
		f.expectCapacityStatusPatch(ct, r, 50, totalReplicaCount, Contender)
		f.run()
	}
}

type role int

const (
	Contender = iota
	Incumbent
)

func TestContenderTrafficShouldIncrease(t *testing.T) {
	for _, i := range interestingReplicaCounts {
		f := newFixture(t)

		totalReplicaCount := i
		contender := buildContender(totalReplicaCount)
		incumbent := buildIncumbent(totalReplicaCount)

		contender.release.Spec.TargetStep = 1
		contender.capacityTarget.Spec.Clusters[0].Percent = 50
		contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = int32(totalReplicaCount)
		contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(totalReplicaCount, 50))

		f.addObjects(contender, incumbent)

		tt := contender.trafficTarget.DeepCopy()
		r := contender.release.DeepCopy()
		f.expectTrafficStatusPatch(tt, r, 50, Contender)
		f.run()
	}
}

func TestIncumbentTrafficShouldDecrease(t *testing.T) {
	for _, i := range interestingReplicaCounts {
		f := newFixture(t)

		totalReplicaCount := i
		contender := buildContender(totalReplicaCount)
		incumbent := buildIncumbent(totalReplicaCount)

		contender.release.Spec.TargetStep = 1
		contender.capacityTarget.Spec.Clusters[0].Percent = 50
		contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = int32(totalReplicaCount)
		contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(totalReplicaCount, 50))
		contender.trafficTarget.Spec.Clusters[0].Weight = 50
		contender.trafficTarget.Status.Clusters[0].AchievedTraffic = 50

		f.addObjects(contender, incumbent)

		tt := incumbent.trafficTarget.DeepCopy()
		r := contender.release.DeepCopy()
		f.expectTrafficStatusPatch(tt, r, 50, Incumbent)
		f.run()
	}
}

func TestIncumbentCapacityShouldDecrease(t *testing.T) {
	for _, i := range interestingReplicaCounts {
		f := newFixture(t)

		totalReplicaCount := i
		contender := buildContender(totalReplicaCount)
		incumbent := buildIncumbent(totalReplicaCount)

		contender.release.Spec.TargetStep = 1
		contender.capacityTarget.Spec.Clusters[0].Percent = 50
		contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = int32(totalReplicaCount)
		contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(totalReplicaCount, 50))
		contender.trafficTarget.Spec.Clusters[0].Weight = 50
		contender.trafficTarget.Status.Clusters[0].AchievedTraffic = 50

		incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
		incumbent.trafficTarget.Status.Clusters[0].AchievedTraffic = 50

		f.addObjects(contender, incumbent)

		tt := incumbent.capacityTarget.DeepCopy()
		r := contender.release.DeepCopy()
		f.expectCapacityStatusPatch(tt, r, 50, totalReplicaCount, Incumbent)
		f.run()
	}
}

func TestContenderReleasePhaseIsWaitingForCommandForFinalStepState(t *testing.T) {
	for _, i := range interestingReplicaCounts {
		f := newFixture(t)

		totalReplicaCount := i
		contender := buildContender(totalReplicaCount)
		incumbent := buildIncumbent(totalReplicaCount)

		contender.release.Spec.TargetStep = 1
		contender.capacityTarget.Spec.Clusters[0].Percent = 50
		contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = int32(totalReplicaCount)
		contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(totalReplicaCount, 50))
		contender.trafficTarget.Spec.Clusters[0].Weight = 50
		contender.trafficTarget.Status.Clusters[0].AchievedTraffic = 50
		incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
		incumbent.trafficTarget.Status.Clusters[0].AchievedTraffic = 50
		incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
		incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = int32(totalReplicaCount)
		incumbent.capacityTarget.Status.Clusters[0].AchievedPercent = 50
		incumbent.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(totalReplicaCount, 50))

		f.addObjects(contender, incumbent)

		rel := contender.release.DeepCopy()
		f.expectReleaseWaitingForCommand(rel, 1)
		f.run()
	}
}

func TestContenderReleaseIsInstalled(t *testing.T) {
	for _, i := range interestingReplicaCounts {
		f := newFixture(t)

		totalReplicaCount := i
		contender := buildContender(totalReplicaCount)
		incumbent := buildIncumbent(totalReplicaCount)

		contender.release.Spec.TargetStep = 2
		contender.capacityTarget.Spec.Clusters[0].Percent = 100
		contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = int32(totalReplicaCount)
		contender.capacityTarget.Status.Clusters[0].AchievedPercent = 100
		contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(totalReplicaCount)
		contender.trafficTarget.Spec.Clusters[0].Weight = 100
		contender.trafficTarget.Status.Clusters[0].AchievedTraffic = 100
		releaseutil.SetReleaseCondition(&contender.release.Status, shipper.ReleaseCondition{Type: shipper.ReleaseConditionTypeInstalled, Status: corev1.ConditionTrue, Reason: "", Message: ""})

		incumbent.trafficTarget.Spec.Clusters[0].Weight = 0
		incumbent.trafficTarget.Status.Clusters[0].AchievedTraffic = 0
		incumbent.capacityTarget.Spec.Clusters[0].Percent = 0
		incumbent.capacityTarget.Status.Clusters[0].AchievedPercent = 0
		incumbent.capacityTarget.Status.Clusters[0].AvailableReplicas = 0

		f.addObjects(contender, incumbent)

		f.expectReleaseReleased(contender.release.DeepCopy(), 2)

		f.run()
	}
}

func workingOnContenderCapacity(percent int, wg *sync.WaitGroup, t *testing.T) {
	defer wg.Done()

	f := newFixture(t)

	totalReplicaCount := uint(10)
	contender := buildContender(totalReplicaCount)
	incumbent := buildIncumbent(totalReplicaCount)

	contender.release.Spec.TargetStep = 1

	achievedCapacityPercentage := 100 - int32(percent)

	// Working on contender capacity.
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = int32(totalReplicaCount)
	contender.capacityTarget.Status.Clusters[0].AchievedPercent = achievedCapacityPercentage
	contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(totalReplicaCount, float64(achievedCapacityPercentage)))

	f.addObjects(contender, incumbent)

	r := contender.release.DeepCopy()
	f.expectCapacityNotReady(r, 1, 0, Contender, "minikube")
	f.run()
}

func workingOnContenderTraffic(percent int, wg *sync.WaitGroup, t *testing.T) {
	defer wg.Done()

	f := newFixture(t)

	totalReplicaCount := uint(10)
	contender := buildContender(totalReplicaCount)
	incumbent := buildIncumbent(totalReplicaCount)

	contender.release.Spec.TargetStep = 1

	// Desired contender capacity achieved.
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = int32(totalReplicaCount)
	contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
	contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(totalReplicaCount, 50))

	// Working on contender traffic.
	contender.trafficTarget.Spec.Clusters[0].Weight = 50
	contender.trafficTarget.Status.Clusters[0].AchievedTraffic = uint32(percent)

	f.addObjects(contender, incumbent)

	r := contender.release.DeepCopy()
	f.expectTrafficNotReady(r, 1, 0, Contender, "minikube")
	f.run()

}

func workingOnIncumbentTraffic(percent int, wg *sync.WaitGroup, t *testing.T) {
	defer wg.Done()

	f := newFixture(t)

	totalReplicaCount := uint(10)
	contender := buildContender(totalReplicaCount)
	incumbent := buildIncumbent(totalReplicaCount)

	contender.release.Spec.TargetStep = 1

	// Desired contender capacity achieved.
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = int32(totalReplicaCount)
	contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
	contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(totalReplicaCount, 50))

	// Desired contender traffic achieved.
	contender.trafficTarget.Spec.Clusters[0].Weight = 50
	contender.trafficTarget.Status.Clusters[0].AchievedTraffic = 50

	// Working on incumbent traffic.
	incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
	incumbent.trafficTarget.Status.Clusters[0].AchievedTraffic = 100 - uint32(percent)

	f.addObjects(contender, incumbent)

	r := contender.release.DeepCopy()
	f.expectTrafficNotReady(r, 1, 0, Incumbent, "minikube")
	f.run()
}

func workingOnIncumbentCapacity(percent int, wg *sync.WaitGroup, t *testing.T) {
	defer wg.Done()

	f := newFixture(t)

	totalReplicaCount := uint(10)
	contender := buildContender(totalReplicaCount)
	incumbent := buildIncumbent(totalReplicaCount)

	contender.release.Spec.TargetStep = 1

	// Desired contender capacity achieved.
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Spec.Clusters[0].TotalReplicaCount = int32(totalReplicaCount)
	contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
	contender.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(totalReplicaCount, 50))

	// Desired contender traffic achieved.
	contender.trafficTarget.Spec.Clusters[0].Weight = 50
	contender.trafficTarget.Status.Clusters[0].AchievedTraffic = 50

	// Desired incumbent traffic achieved.
	incumbent.trafficTarget.Spec.Clusters[0].Weight = 50
	incumbent.trafficTarget.Status.Clusters[0].AchievedTraffic = 50

	// Working on incumbent capacity.
	incumbentAchievedCapacityPercentage := 100 - int32(percent)
	incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
	incumbent.capacityTarget.Spec.Clusters[0].TotalReplicaCount = int32(totalReplicaCount)
	incumbent.capacityTarget.Status.Clusters[0].AchievedPercent = incumbentAchievedCapacityPercentage
	incumbent.capacityTarget.Status.Clusters[0].AvailableReplicas = int32(replicas.CalculateDesiredReplicaCount(totalReplicaCount, float64(incumbentAchievedCapacityPercentage)))

	f.addObjects(contender, incumbent)

	r := contender.release.DeepCopy()
	f.expectCapacityNotReady(r, 1, 0, Incumbent, "minikube")
	f.run()
}

func TestShouldNotProducePatches(t *testing.T) {
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go workingOnContenderCapacity(i, &wg, t)

		wg.Add(1)
		go workingOnContenderTraffic(i, &wg, t)

		wg.Add(1)
		go workingOnIncumbentTraffic(i, &wg, t)

		wg.Add(1)
		go workingOnIncumbentCapacity(i, &wg, t)
	}
	wg.Wait()
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

func (f *fixture) newController() (*Controller, shipperinformers.SharedInformerFactory) {

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

	c := NewController(f.client, shipperInformerFactory, f.clientPool, f.recorder)

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
		func() (bool, error) { return c.releaseWorkqueue.Len() >= 1, nil },
		stopCh,
	)

	readyCh := make(chan struct{})
	go func() {
		for e := range f.recorder.Events {
			f.receivedEvents = append(f.receivedEvents, e)
		}
		close(readyCh)
	}()

	c.processNextReleaseWorkItem()
	c.processNextAppWorkItem()
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
