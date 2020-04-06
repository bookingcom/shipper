package release

import (
	"fmt"
	intstrutil "k8s.io/apimachinery/pkg/util/intstr"
	"math"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	helmchart "k8s.io/helm/pkg/proto/hapi/chart"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperchart "github.com/bookingcom/shipper/pkg/chart"
	shipperrepo "github.com/bookingcom/shipper/pkg/chart/repo"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/controller"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

type Scheduler struct {
	clientset shipperclientset.Interface

	installationTargetLister listers.InstallationTargetLister
	trafficTargetLister      listers.TrafficTargetLister
	capacityTargetLister     listers.CapacityTargetLister

	chartFetcher shipperrepo.ChartFetcher

	recorder record.EventRecorder
}

func NewScheduler(
	clientset shipperclientset.Interface,
	installationTargerLister listers.InstallationTargetLister,
	capacityTargetLister listers.CapacityTargetLister,
	trafficTargetLister listers.TrafficTargetLister,
	chartFetcher shipperrepo.ChartFetcher,
	recorder record.EventRecorder,
) *Scheduler {
	return &Scheduler{
		clientset: clientset,

		installationTargetLister: installationTargerLister,
		trafficTargetLister:      trafficTargetLister,
		capacityTargetLister:     capacityTargetLister,

		chartFetcher: chartFetcher,

		recorder: recorder,
	}
}

func (s *Scheduler) ScheduleRelease(rel *shipper.Release) (*releaseInfo, error) {
	replicaCount, err := s.fetchChartAndExtractReplicaCount(rel)
	if err != nil {
		return nil, err
	}

	releaseErrors := shippererrors.NewMultiError()
	if rel.Spec.VirtualStrategy == nil {
		virtualStrategy, err := buildVirtualStrategy(rel, replicaCount)
		if err != nil {
			//klog.Infof("HILLA COULD NOT CALCULATE VIRTUAL STRATEGY!!!")
			return nil, err
		}

		//klog.Infof("HILLA about to put virtual strategy into %s", rel.Name)
		rel.Spec.VirtualStrategy = virtualStrategy
		//klog.Infof("HILLA DID IT!  virtual strategy is %v", virtualStrategy)
		//return &releaseInfo{
		//	release:            rel,
		//	installationTarget: nil,
		//	trafficTarget:      nil,
		//	capacityTarget:     nil,
		//}, shippererrors.NewUpdateVirtualStrategy(controller.MetaKey(rel))
	}

	it, err := s.CreateOrUpdateInstallationTarget(rel)
	if err != nil {
		releaseErrors.Append(err)
	}

	tt, err := s.CreateOrUpdateTrafficTarget(rel)
	if err != nil {
		releaseErrors.Append(err)
	}

	ct, err := s.CreateOrUpdateCapacityTarget(rel, replicaCount)
	if err != nil {
		releaseErrors.Append(err)
	}

	if releaseErrors.Any() {
		return nil, releaseErrors.Flatten()
	}

	return &releaseInfo{
		release:            rel,
		installationTarget: it,
		trafficTarget:      tt,
		capacityTarget:     ct,
	}, nil
}

func stringSliceEqual(arr1, arr2 []string) bool {
	if len(arr1) != len(arr2) {
		return false
	}
	for i := 0; i < len(arr1); i++ {
		if arr1[i] != arr2[i] {
			return false
		}
	}

	return true
}

// getReleaseClusters is a helper function that returns a list of cluster names
// annotating the release. It assumes cluster names are all unique.
func getReleaseClusters(rel *shipper.Release) []string {
	allRelClusters := strings.Split(rel.ObjectMeta.Annotations[shipper.ReleaseClustersAnnotation], ",")
	if len(allRelClusters) == 1 && allRelClusters[0] == "" {
		allRelClusters = []string{}
	}
	uniqRelClusters := make([]string, 0, len(allRelClusters))
	seen := make(map[string]struct{})
	for _, cluster := range allRelClusters {
		if _, ok := seen[cluster]; !ok {
			uniqRelClusters = append(uniqRelClusters, cluster)
			seen[cluster] = struct{}{}
		}
	}

	sort.Strings(uniqRelClusters)

	return uniqRelClusters
}

// The 3 functions below are based on a basic cluster name set match, and never
// take into account a cluster weight change. This must be addressed in the
// future.
func installationTargetClustersMatch(it *shipper.InstallationTarget, clusters []string) bool {
	itClusters := it.Spec.Clusters
	sort.Strings(itClusters)

	return stringSliceEqual(clusters, itClusters)
}

func capacityTargetClustersMatch(ct *shipper.CapacityTarget, clusters []string) bool {
	ctClusters := make([]string, 0, len(ct.Spec.Clusters))
	for _, ctc := range ct.Spec.Clusters {
		ctClusters = append(ctClusters, ctc.Name)
	}
	sort.Strings(ctClusters)

	return stringSliceEqual(clusters, ctClusters)
}

func trafficTargetClustersMatch(tt *shipper.TrafficTarget, clusters []string) bool {
	ttClusters := make([]string, 0, len(tt.Spec.Clusters))
	for _, ttc := range tt.Spec.Clusters {
		ttClusters = append(ttClusters, ttc.Name)
	}
	sort.Strings(ttClusters)

	return stringSliceEqual(clusters, ttClusters)
}

func setInstallationTargetClusters(it *shipper.InstallationTarget, clusters []string) {
	it.Spec.Clusters = clusters
}

func setCapacityTargetClusters(ct *shipper.CapacityTarget, clusters []string, totalReplicaCount int32) {
	capacityTargetClusters := make([]shipper.ClusterCapacityTarget, 0, len(clusters))
	for _, cluster := range clusters {
		capacityTargetClusters = append(
			capacityTargetClusters,
			shipper.ClusterCapacityTarget{
				Name:              cluster,
				Percent:           0,
				TotalReplicaCount: totalReplicaCount,
			})
	}
	ct.Spec.Clusters = capacityTargetClusters
}

func setTrafficTargetClusters(tt *shipper.TrafficTarget, clusters []string) {
	trafficTargetClusters := make([]shipper.ClusterTrafficTarget, 0, len(clusters))
	for _, cluster := range clusters {
		trafficTargetClusters = append(
			trafficTargetClusters,
			shipper.ClusterTrafficTarget{
				Name:   cluster,
				Weight: 0,
			})
	}
	tt.Spec.Clusters = trafficTargetClusters
}

func (s *Scheduler) CreateOrUpdateInstallationTarget(rel *shipper.Release) (*shipper.InstallationTarget, error) {
	clusters := getReleaseClusters(rel)

	it, err := s.installationTargetLister.InstallationTargets(rel.GetNamespace()).Get(rel.GetName())
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}
		it := &shipper.InstallationTarget{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rel.Name,
				Namespace: rel.Namespace,
				Labels:    rel.Labels,
				OwnerReferences: []metav1.OwnerReference{
					createOwnerRefFromRelease(rel),
				},
			},
			Spec: shipper.InstallationTargetSpec{
				Chart:       rel.Spec.Environment.Chart,
				Values:      rel.Spec.Environment.Values,
				CanOverride: true,
			},
		}
		setInstallationTargetClusters(it, clusters)

		updIt, err := s.clientset.ShipperV1alpha1().InstallationTargets(rel.GetNamespace()).Create(it)
		if err != nil {
			return nil, shippererrors.NewKubeclientCreateError(it, err)
		}

		s.recorder.Eventf(
			rel,
			corev1.EventTypeNormal,
			"ReleaseScheduled",
			"Created InstallationTarget %q",
			controller.MetaKey(updIt),
		)

		return updIt, nil
	}

	if !objectBelongsToRelease(it, rel) {
		return nil, shippererrors.NewWrongOwnerReferenceError(it, rel)
	}

	if !installationTargetClustersMatch(it, clusters) {
		setInstallationTargetClusters(it, clusters)
		updIt, err := s.clientset.ShipperV1alpha1().InstallationTargets(rel.GetNamespace()).Update(it)
		if err != nil {
			return nil, err
		}
		s.recorder.Eventf(
			rel,
			corev1.EventTypeNormal,
			"ReleaseScheduled",
			"Updated InstallationTarget %q cluster set to [%s]",
			controller.MetaKey(updIt),
			strings.Join(clusters, ","))
		return updIt, nil
	}

	return it, nil
}

func (s *Scheduler) CreateOrUpdateCapacityTarget(rel *shipper.Release, totalReplicaCount int32) (*shipper.CapacityTarget, error) {
	clusters := getReleaseClusters(rel)

	ct, err := s.capacityTargetLister.CapacityTargets(rel.GetNamespace()).Get(rel.GetName())
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}
		ct := &shipper.CapacityTarget{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rel.Name,
				Namespace: rel.Namespace,
				Labels:    rel.Labels,
				OwnerReferences: []metav1.OwnerReference{
					createOwnerRefFromRelease(rel),
				},
			},
		}
		setCapacityTargetClusters(ct, clusters, totalReplicaCount)

		updCt, err := s.clientset.ShipperV1alpha1().CapacityTargets(rel.GetNamespace()).Create(ct)
		if err != nil {
			return nil, shippererrors.NewKubeclientCreateError(ct, err)
		}

		s.recorder.Eventf(
			rel,
			corev1.EventTypeNormal,
			"ReleaseScheduled",
			"Created CapacityTarget %q",
			controller.MetaKey(updCt),
		)

		return updCt, nil
	}

	if !objectBelongsToRelease(ct, rel) {
		return nil, shippererrors.NewWrongOwnerReferenceError(ct, rel)
	}

	if !capacityTargetClustersMatch(ct, clusters) {
		setCapacityTargetClusters(ct, clusters, totalReplicaCount)
		updCt, err := s.clientset.ShipperV1alpha1().CapacityTargets(rel.GetNamespace()).Update(ct)
		if err != nil {
			return nil, err
		}
		s.recorder.Eventf(
			rel,
			corev1.EventTypeNormal,
			"ReleaseScheduled",
			"Updated CapacityTarget %q cluster set to [%s]",
			controller.MetaKey(updCt),
			strings.Join(clusters, ","))
		return updCt, nil
	}

	return ct, nil
}

func (s *Scheduler) CreateOrUpdateTrafficTarget(rel *shipper.Release) (*shipper.TrafficTarget, error) {
	clusters := getReleaseClusters(rel)

	tt, err := s.trafficTargetLister.TrafficTargets(rel.GetNamespace()).Get(rel.GetName())
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}
		tt := &shipper.TrafficTarget{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rel.Name,
				Namespace: rel.Namespace,
				Labels:    rel.Labels,
				OwnerReferences: []metav1.OwnerReference{
					createOwnerRefFromRelease(rel),
				},
			},
		}
		setTrafficTargetClusters(tt, clusters)

		updTt, err := s.clientset.ShipperV1alpha1().TrafficTargets(rel.GetNamespace()).Create(tt)
		if err != nil {
			return nil, shippererrors.NewKubeclientCreateError(tt, err)
		}

		s.recorder.Eventf(
			rel,
			corev1.EventTypeNormal,
			"ReleaseScheduled",
			"Created TrafficTarget %q",
			controller.MetaKey(updTt),
		)

		return updTt, nil
	}

	if !objectBelongsToRelease(tt, rel) {
		return nil, shippererrors.NewWrongOwnerReferenceError(tt, rel)
	}

	if !trafficTargetClustersMatch(tt, clusters) {
		setTrafficTargetClusters(tt, clusters)
		updTt, err := s.clientset.ShipperV1alpha1().TrafficTargets(rel.GetNamespace()).Update(tt)
		if err != nil {
			return nil, err
		}
		s.recorder.Eventf(
			rel,
			corev1.EventTypeNormal,
			"ReleaseScheduled",
			"Updated TrafficTarget %q cluster set to [%s]",
			controller.MetaKey(updTt),
			strings.Join(clusters, ","))
		return updTt, nil
	}

	return tt, nil
}

func (s *Scheduler) fetchChartAndExtractReplicaCount(rel *shipper.Release) (int32, error) {
	chart, err := s.chartFetcher(&rel.Spec.Environment.Chart)
	if err != nil {
		return 0, err
	}

	replicas, err := extractReplicasFromChartForRel(chart, rel)
	if err != nil {
		return 0, err
	}

	return int32(replicas), nil
}

func extractReplicasFromChartForRel(chart *helmchart.Chart, rel *shipper.Release) (int32, error) {
	owners := rel.OwnerReferences
	if l := len(owners); l != 1 {
		return 0, shippererrors.NewMultipleOwnerReferencesError(rel.Name, l)
	}

	applicationName := owners[0].Name
	rendered, err := shipperchart.Render(chart, applicationName, rel.Namespace, &rel.Spec.Environment.Values)
	if err != nil {
		return 0, shippererrors.NewBrokenChartSpecError(
			&rel.Spec.Environment.Chart,
			err,
		)
	}

	deployments := shipperchart.GetDeployments(rendered)
	if len(deployments) != 1 {
		return 0, shippererrors.NewWrongChartDeploymentsError(
			&rel.Spec.Environment.Chart,
			len(deployments),
		)
	}

	replicas := deployments[0].Spec.Replicas
	// Deployments default to 1 replica when replicas is nil or unspecified. See
	// k8s.io/api/apps/v1/types.go's DeploymentSpec.
	if replicas == nil {
		return 1, nil
	}

	return int32(*replicas), nil
}

// The strings here are insane, but if you create a fresh release object for
// some reason it lands in the work queue with an empty TypeMeta. This is
// resolved if you restart the controllers, so I'm not sure what's going on.
// https://github.com/kubernetes/client-go/issues/60#issuecomment-281533822 and
// https://github.com/kubernetes/client-go/issues/60#issuecomment-281747911 give
// some potential context.
func createOwnerRefFromRelease(r *shipper.Release) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: shipper.SchemeGroupVersion.String(),
		Kind:       "Release",
		Name:       r.GetName(),
		UID:        r.GetUID(),
	}
}

func objectBelongsToRelease(obj metav1.Object, release *shipper.Release) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.Kind == "Release" && ref.UID == release.UID {
			return true
		}
	}

	return false
}

func buildVirtualStrategy(rel *shipper.Release, replicaCount int32) (*shipper.RolloutVirtualStrategy, error) {
	strategy := rel.Spec.Environment.Strategy
	rolloutVirtualStrategy := make([]shipper.RolloutStrategyVirtualStep, len(strategy.Steps))
	//rolloutBackVirtualStrategy := make([]shipper.RolloutStrategyVirtualStep, len(strategy.Steps)-1)

	surgeWeight, err := getMaxSurgePercent(rel, replicaCount)
	if err != nil {
		return nil, err
	}

	// create the first virtual transition: from previous release to first strategy step
	step := shipper.RolloutStrategyStep{
		Name: "",
		Capacity: shipper.RolloutStrategyStepValue{
			Incumbent: 100,
			Contender: 0,
		},
		Traffic: shipper.RolloutStrategyStepValue{
			Incumbent: 100,
			Contender: 0,
		},
	}
	nextStep := strategy.Steps[0]
	rolloutStrategyVirtualSteps := buildRolloutStrategyVirtualSteps(step, nextStep, surgeWeight)
	rolloutVirtualStrategy[0] = shipper.RolloutStrategyVirtualStep{VirtualSteps: rolloutStrategyVirtualSteps}

	for i, step := range strategy.Steps {
		if i != len(strategy.Steps)-1 {
			nextStep := strategy.Steps[i+1]
			rolloutStrategyVirtualSteps := buildRolloutStrategyVirtualSteps(step, nextStep, surgeWeight)

			rolloutVirtualStrategy[i+1] = shipper.RolloutStrategyVirtualStep{VirtualSteps: rolloutStrategyVirtualSteps}
		}
	}
	virtualStrategy := &shipper.RolloutVirtualStrategy{Steps: rolloutVirtualStrategy}
	return virtualStrategy, nil
}

func buildRolloutStrategyVirtualSteps(step, nextStep shipper.RolloutStrategyStep, surgeWeight int) []shipper.RolloutStrategyStep {
	// Calculate virtual steps
	// Contender:
	currContenderCapacity := step.Capacity.Contender
	currContenderTraffic := step.Traffic.Contender
	nextContenderCapacity := nextStep.Capacity.Contender
	nextContenderTraffic := nextStep.Traffic.Contender
	diffContenderCapacity := float64(nextContenderCapacity - currContenderCapacity)
	virtualStepsContender := math.Ceil(diffContenderCapacity / float64(surgeWeight))

	// Incumbent:
	currIncumbentCapacity := step.Capacity.Incumbent
	currIncumbentTraffic := step.Traffic.Incumbent
	nextIncumbentCapacity := nextStep.Capacity.Incumbent
	nextIncumbentTraffic := nextStep.Traffic.Incumbent
	diffIncumbentCapacity := float64(currIncumbentCapacity - nextIncumbentCapacity)
	virtualStepsIncumbent := math.Ceil(diffIncumbentCapacity / float64(surgeWeight))

	virtualSteps := math.Max(virtualStepsContender, virtualStepsIncumbent)
	virtualSteps = math.Max(virtualSteps, 1)

	// Calculate traffic surge to each step
	diffContenderTraffic := float64(nextContenderTraffic - currContenderTraffic)
	diffIncumbentTraffic := float64(nextIncumbentTraffic - currIncumbentTraffic)
	trafficSurgeContender := math.Ceil(diffContenderTraffic / virtualSteps)
	trafficSurgeIncumvent := math.Ceil(diffIncumbentTraffic / virtualSteps)
	trafficSurge := int(math.Max(trafficSurgeContender, trafficSurgeIncumvent))

	rolloutStrategyVirtualSteps := make([]shipper.RolloutStrategyStep, int(virtualSteps)+1)
	for j, _ := range rolloutStrategyVirtualSteps {
		multiplier := j // + 1
		// Contender:
		// Capacity
		possibleContenderCapacity := currContenderCapacity + int32(multiplier*surgeWeight)
		newContenderCapacity := int32(math.Min(float64(possibleContenderCapacity), float64(nextContenderCapacity)))
		// Traffic
		possibleContenderTraffic := currContenderTraffic + int32(multiplier*trafficSurge)
		newContenderTraffic := int32(math.Min(float64(possibleContenderTraffic), float64(nextContenderTraffic)))

		// Incumbent:
		// Capacity
		possibleIncumbentCapacity := currIncumbentCapacity - int32(multiplier*surgeWeight)
		newIncumbentCapacity := int32(math.Max(float64(possibleIncumbentCapacity), float64(nextIncumbentCapacity)))
		// Traffic
		possibleIncumbentTraffic := currIncumbentTraffic - int32(multiplier*trafficSurge)
		newIncumbentTraffic := int32(math.Max(float64(possibleIncumbentTraffic), float64(nextIncumbentTraffic)))

		rolloutStrategyVirtualSteps[j] = shipper.RolloutStrategyStep{
			Name: fmt.Sprintf("%d: %s to %s", j, step.Name, nextStep.Name),
			Capacity: shipper.RolloutStrategyStepValue{
				Incumbent: newIncumbentCapacity,
				Contender: newContenderCapacity,
			},
			Traffic: shipper.RolloutStrategyStepValue{
				Incumbent: newIncumbentTraffic,
				Contender: newContenderTraffic,
			},
		}
	}
	return rolloutStrategyVirtualSteps
}

func getMaxSurgePercent(rel *shipper.Release, replicaCount int32) (int, error) {
	//return 20, nil
	maxSurgeValue := intstrutil.FromString("100%")
	strategy := rel.Spec.Environment.Strategy
	if strategy != nil {
		rollingUpdate := strategy.RollingUpdate
		if rollingUpdate != nil {
			maxSurgeValue = *intstrutil.ValueOrDefault(&rollingUpdate.MaxSurge, maxSurgeValue)
		}
	}

	surge, err := intstrutil.GetValueFromIntOrPercent(&maxSurgeValue, int(replicaCount), true)
	if err != nil {
		return 0, err
	}
	surgePercent := (float64(surge) / float64(replicaCount)) * 100
	surgePercent = math.Min(surgePercent, 100) // cap by 100
	return int(surgePercent), err
}
