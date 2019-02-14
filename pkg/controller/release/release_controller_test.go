package release

import (
	"encoding/json"
	"fmt"
	"math/rand"
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
	receivedEvents []string
	expectedEvents []string
}

func newFixture(t *testing.T, objects ...runtime.Object) *fixture {
	return &fixture{
		initialized: false,
		t:           t,
		objects:     objects,

		actions:        make([]kubetesting.Action, 0),
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

	fmt.Println("processing the next release item")
	b := controller.processNextReleaseWorkItem()
	fmt.Printf("process next item continue: %t\n", b)
	//controller.processNextAppWorkItem()
	close(f.recorder.Events)
	fmt.Println("done processing")
	<-readyCh
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

	capacityTargetStatusClusters := make([]shipper.ClusterCapacityStatus, len(clusterNames))
	capacityTargetSpecClusters := make([]shipper.ClusterCapacityTarget, len(clusterNames))
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

	trafficTargetStatusClusters := make([]*shipper.ClusterTrafficStatus, len(clusterNames))
	trafficTargetSpecClusters := make([]shipper.ClusterTrafficTarget, len(clusterNames))

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

	capacityTargetStatusClusters := make([]shipper.ClusterCapacityStatus, len(clusterNames))
	capacityTargetSpecClusters := make([]shipper.ClusterCapacityTarget, len(clusterNames))
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

	trafficTargetStatusClusters := make([]*shipper.ClusterTrafficStatus, len(clusterNames))
	trafficTargetSpecClusters := make([]shipper.ClusterTrafficTarget, len(clusterNames))

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
		`Normal ReleaseStateTransitioned Release "test-namespace/0.0.2" had its state "WaitingForCapacity" transitioned to "False"`,
		`Normal ReleaseStateTransitioned Release "test-namespace/0.0.2" had its state "WaitingForCommand" transitioned to "True"`,
		`Normal ReleaseStateTransitioned Release "test-namespace/0.0.2" had its state "WaitingForInstallation" transitioned to "False"`,
		`Normal ReleaseStateTransitioned Release "test-namespace/0.0.2" had its state "WaitingForTraffic" transitioned to "False"`,
	}
}

func TestContenderReleasePhaseIsWaitingForCommandForInitialStepState(t *testing.T) {
	namespace := "test-namespace"
	app := buildApplication(namespace, "test-app")
	cluster := buildCluster("minikube")

	for _, replicaCount := range []int32{1, 3, 10} {
		f := newFixture(t, app.DeepCopy(), cluster.DeepCopy())
		incumbentName, contenderName := randStr(), randStr()
		app.Status.History = []string{incumbentName, contenderName}
		incumbent := f.buildIncumbent(namespace, incumbentName, replicaCount)
		contender := f.buildContender(namespace, contenderName, replicaCount)

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
