package strategy

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubetesting "k8s.io/client-go/testing"
)

func TestContenderReleasePhaseIsWaitingForCommandForInitialStepState(t *testing.T) {
	f := newFixture(t)

	contender := buildContender()
	incumbent := buildIncumbent()

	f.addObjects(contender, incumbent)

	rel := contender.release.DeepCopy()
	f.expectReleasePhaseWaitingForCommand(rel, 0)
	f.run()
}

func TestContenderCapacityShouldIncrease(t *testing.T) {
	f := newFixture(t)

	contender := buildContender()
	incumbent := buildIncumbent()

	contender.release.Spec.TargetStep = 1

	f.addObjects(contender, incumbent)

	ct := contender.capacityTarget.DeepCopy()
	f.expectCapacityStatusPatch(ct, 50)
	f.run()
}

func TestContenderTrafficShouldIncrease(t *testing.T) {
	f := newFixture(t)

	contender := buildContender()
	incumbent := buildIncumbent()

	contender.release.Spec.TargetStep = 1
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50

	f.addObjects(contender, incumbent)

	tt := contender.trafficTarget.DeepCopy()
	f.expectTrafficStatusPatch(tt, 50)
	f.run()
}

func TestIncumbentTrafficShouldDecrease(t *testing.T) {
	f := newFixture(t)

	contender := buildContender()
	incumbent := buildIncumbent()

	contender.release.Spec.TargetStep = 1
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
	contender.trafficTarget.Spec.Clusters[0].TargetTraffic = 50
	contender.trafficTarget.Status.Clusters[0].AchievedTraffic = 50

	f.addObjects(contender, incumbent)

	tt := incumbent.trafficTarget.DeepCopy()
	f.expectTrafficStatusPatch(tt, 50)
	f.run()
}

func TestIncumbentCapacityShouldDecrease(t *testing.T) {
	f := newFixture(t)

	contender := buildContender()
	incumbent := buildIncumbent()

	contender.release.Spec.TargetStep = 1
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
	contender.trafficTarget.Spec.Clusters[0].TargetTraffic = 50
	contender.trafficTarget.Status.Clusters[0].AchievedTraffic = 50
	incumbent.trafficTarget.Spec.Clusters[0].TargetTraffic = 50
	incumbent.trafficTarget.Status.Clusters[0].AchievedTraffic = 50

	f.addObjects(contender, incumbent)

	tt := incumbent.capacityTarget.DeepCopy()
	f.expectCapacityStatusPatch(tt, 50)
	f.run()
}

func TestContenderReleasePhaseIsWaitingForCommandForFinalStepState(t *testing.T) {
	f := newFixture(t)

	contender := buildContender()
	incumbent := buildIncumbent()

	contender.release.Spec.TargetStep = 1
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50
	contender.trafficTarget.Spec.Clusters[0].TargetTraffic = 50
	contender.trafficTarget.Status.Clusters[0].AchievedTraffic = 50
	incumbent.trafficTarget.Spec.Clusters[0].TargetTraffic = 50
	incumbent.trafficTarget.Status.Clusters[0].AchievedTraffic = 50
	incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
	incumbent.capacityTarget.Status.Clusters[0].AchievedPercent = 50

	f.addObjects(contender, incumbent)

	rel := contender.release.DeepCopy()
	f.expectReleasePhaseWaitingForCommand(rel, 1)
	f.run()
}

func TestContenderReleaseIsInstalled(t *testing.T) {
	f := newFixture(t)

	contender := buildContender()
	incumbent := buildIncumbent()

	contender.release.Spec.TargetStep = 2
	contender.capacityTarget.Spec.Clusters[0].Percent = 100
	contender.capacityTarget.Status.Clusters[0].AchievedPercent = 100
	contender.trafficTarget.Spec.Clusters[0].TargetTraffic = 100
	contender.trafficTarget.Status.Clusters[0].AchievedTraffic = 100
	incumbent.trafficTarget.Spec.Clusters[0].TargetTraffic = 0
	incumbent.trafficTarget.Status.Clusters[0].AchievedTraffic = 0
	incumbent.capacityTarget.Spec.Clusters[0].Percent = 0
	incumbent.capacityTarget.Status.Clusters[0].AchievedPercent = 0

	f.addObjects(contender, incumbent)

	f.expectReleaseInstalled(contender.release.DeepCopy())
	f.expectReleaseSuperseded(incumbent.release.DeepCopy())

	f.run()
}

func workingOnContenderCapacity(percent int, wg *sync.WaitGroup, t *testing.T) {
	defer wg.Done()

	f := newFixture(t)

	contender := buildContender()
	incumbent := buildIncumbent()

	contender.release.Spec.TargetStep = 1

	// Working on contender capacity
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Status.Clusters[0].AchievedPercent = int32(percent)

	f.addObjects(contender, incumbent)

	f.run()
}

func workingOnContenderTraffic(percent int, wg *sync.WaitGroup, t *testing.T) {
	defer wg.Done()

	f := newFixture(t)

	contender := buildContender()
	incumbent := buildIncumbent()

	contender.release.Spec.TargetStep = 1

	// Desired contender capacity achieved
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50

	// Working on contender traffic
	contender.trafficTarget.Spec.Clusters[0].TargetTraffic = 50
	contender.trafficTarget.Status.Clusters[0].AchievedTraffic = uint(percent)

	f.addObjects(contender, incumbent)

	f.run()

}

func workingOnIncumbentTraffic(percent int, wg *sync.WaitGroup, t *testing.T) {
	defer wg.Done()

	f := newFixture(t)

	contender := buildContender()
	incumbent := buildIncumbent()

	contender.release.Spec.TargetStep = 1

	// Desired contender capacity achieved
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50

	// Desired contender traffic achieved
	contender.trafficTarget.Spec.Clusters[0].TargetTraffic = 50
	contender.trafficTarget.Status.Clusters[0].AchievedTraffic = 50

	// Working on incumbent traffic
	incumbent.trafficTarget.Spec.Clusters[0].TargetTraffic = 50
	incumbent.trafficTarget.Status.Clusters[0].AchievedTraffic = 100 - uint(percent)

	f.addObjects(contender, incumbent)

	f.run()
}

func workingOnIncumbentCapacity(percent int, wg *sync.WaitGroup, t *testing.T) {
	defer wg.Done()

	f := newFixture(t)

	contender := buildContender()
	incumbent := buildIncumbent()

	contender.release.Spec.TargetStep = 1

	// Desired contender capacity achieved
	contender.capacityTarget.Spec.Clusters[0].Percent = 50
	contender.capacityTarget.Status.Clusters[0].AchievedPercent = 50

	// Desired contender traffic achieved
	contender.trafficTarget.Spec.Clusters[0].TargetTraffic = 50
	contender.trafficTarget.Status.Clusters[0].AchievedTraffic = 50

	// Desired incumbent traffic achieved
	incumbent.trafficTarget.Spec.Clusters[0].TargetTraffic = 50
	incumbent.trafficTarget.Status.Clusters[0].AchievedTraffic = 50

	// Working on incumbent capacity
	incumbent.capacityTarget.Spec.Clusters[0].Percent = 50
	incumbent.capacityTarget.Status.Clusters[0].AchievedPercent = 100 - int32(percent)

	f.addObjects(contender, incumbent)

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
	t          *testing.T
	client     *shipperfake.Clientset
	discovery  *fakediscovery.FakeDiscovery
	clientPool *dynamicfake.FakeClientPool
	actions    []kubetesting.Action
	objects    []runtime.Object
}

func newFixture(t *testing.T) *fixture {
	return &fixture{t: t}
}

func (f *fixture) addObjects(releaseInfos ...*releaseInfo) {
	for _, r := range releaseInfos {
		f.objects = append(f.objects,
			r.release,
			r.capacityTarget,
			r.installationTarget,
			r.trafficTarget)
	}
}

func (f *fixture) newController() (*Controller, shipperinformers.SharedInformerFactory) {

	f.client = shipperfake.NewSimpleClientset(f.objects...)

	fakeDiscovery, _ := f.client.Discovery().(*fakediscovery.FakeDiscovery)
	f.discovery = fakeDiscovery
	f.discovery.Resources = []*v1.APIResourceList{
		{
			GroupVersion: "shipper.booking.com/v1",
			APIResources: []v1.APIResource{
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

	f.clientPool = &dynamicfake.FakeClientPool{
		Fake: f.client.Fake,
	}

	c := NewController(f.client, shipperInformerFactory, f.clientPool)

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
		func() (bool, error) { return c.workqueue.Len() >= 1, nil },
		stopCh,
	)

	c.processNextWorkItem()

	actual := shippertesting.FilterActions(f.clientPool.Actions())
	shippertesting.CheckActions(f.actions, actual, f.t)
}

func (f *fixture) expectCapacityStatusPatch(ct *shipperV1.CapacityTarget, value uint) {
	gvr := shipperV1.SchemeGroupVersion.WithResource("capacitytargets")
	newSpec := map[string]interface{}{
		"spec": shipperV1.CapacityTargetSpec{
			Clusters: []shipperV1.ClusterCapacityTarget{
				{Name: "minikube", Percent: int32(value)},
			},
		},
	}
	patch, _ := json.Marshal(newSpec)
	action := kubetesting.NewPatchAction(gvr, ct.GetNamespace(), ct.GetName(), patch)

	f.actions = append(f.actions, action)
}

func (f *fixture) expectTrafficStatusPatch(tt *shipperV1.TrafficTarget, value uint) {
	gvr := shipperV1.SchemeGroupVersion.WithResource("traffictargets")
	newSpec := map[string]interface{}{
		"spec": shipperV1.TrafficTargetSpec{
			Clusters: []shipperV1.ClusterTrafficTarget{
				{Name: "minikube", TargetTraffic: value},
			},
		},
	}
	patch, _ := json.Marshal(newSpec)
	action := kubetesting.NewPatchAction(gvr, tt.GetNamespace(), tt.GetName(), patch)

	f.actions = append(f.actions, action)
}

func (f *fixture) expectReleaseSuperseded(rel *shipperV1.Release) {
	gvr := shipperV1.SchemeGroupVersion.WithResource("releases")
	newStatus := map[string]interface{}{
		"status": shipperV1.ReleaseStatus{
			AchievedStep: rel.Status.AchievedStep,
			Phase:        shipperV1.ReleasePhaseSuperseded,
		},
	}

	patch, _ := json.Marshal(newStatus)
	action := kubetesting.NewPatchAction(gvr, rel.GetNamespace(), rel.GetName(), patch)

	f.actions = append(f.actions, action)
}

func (f *fixture) expectReleaseInstalled(rel *shipperV1.Release) {
	gvr := shipperV1.SchemeGroupVersion.WithResource("releases")
	newStatus := map[string]interface{}{
		"status": shipperV1.ReleaseStatus{
			AchievedStep: uint(rel.Spec.TargetStep),
			Phase:        shipperV1.ReleasePhaseInstalled,
		},
	}

	patch, _ := json.Marshal(newStatus)
	action := kubetesting.NewPatchAction(gvr, rel.GetNamespace(), rel.GetName(), patch)

	f.actions = append(f.actions, action)
}

func (f *fixture) expectReleasePhaseWaitingForCommand(rel *shipperV1.Release, step uint) {
	gvr := shipperV1.SchemeGroupVersion.WithResource("releases")
	newStatus := map[string]interface{}{
		"status": shipperV1.ReleaseStatus{
			AchievedStep: step,
			Phase:        shipperV1.ReleasePhaseWaitingForCommand,
		},
	}

	patch, _ := json.Marshal(newStatus)
	action := kubetesting.NewPatchAction(gvr, rel.GetNamespace(), rel.GetName(), patch)

	f.actions = append(f.actions, action)
}
