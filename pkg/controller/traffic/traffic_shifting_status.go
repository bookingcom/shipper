package traffic

import (
	"math"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	"github.com/bookingcom/shipper/pkg/util/replicas"
)

type clusterReleaseWeights map[string]map[string]uint32

type trafficShiftingStatus struct {
	ready                 bool
	achievedTrafficWeight uint32
	podsReady             int
	podsNotReady          int
	podsLabeled           int
	podsToShift           map[string][]*corev1.Pod
}

// buildTrafficShiftingStatus looks at the current state of a cluster regarding
// the progression of traffic shifting. It's concerned with how many of the
// available pods have been labeled to receive traffic, how many are actually
// ready according to the state of the Endpoints object, and the currently
// achieved weight for a release. If the current state is different from the
// desired one, it also returns which pods need to receive which labels to move
// forward.
func buildTrafficShiftingStatus(
	cluster, appName, releaseName string,
	clusterReleaseWeights clusterReleaseWeights,
	endpoints *corev1.Endpoints,
	appPods []*corev1.Pod,
) trafficShiftingStatus {
	releaseTargetWeights, ok := clusterReleaseWeights[cluster]
	if !ok {
		return trafficShiftingStatus{}
	}

	releaseSelector := labels.Set(map[string]string{
		shipper.AppLabel:     appName,
		shipper.ReleaseLabel: releaseName,
	}).AsSelector()

	podsByTrafficStatus, podsInRelease, podsReady, podsNotReady := summarizePods(
		appPods, endpoints, releaseSelector)

	releaseTargetWeight := releaseTargetWeights[releaseName]
	totalTargetWeight := uint32(0)
	for _, weight := range releaseTargetWeights {
		totalTargetWeight += weight
	}

	podsInApp := len(appPods)
	podsLabeledForTraffic := len(podsByTrafficStatus[shipper.Enabled])
	podsToLabel := calculateReleasePodTarget(
		podsInRelease, releaseTargetWeight, podsInApp, totalTargetWeight)

	// A TrafficTarget is ready when it has achieved a certain number of
	// pods, not a certain weight. That's because its number of pods is
	// capped by the amount of pods in the release, which may be less than
	// what the weight would require.
	ready := podsReady == podsToLabel

	var podsToShift map[string][]*corev1.Pod
	if !ready {
		podsToShift = buildPodsToShift(podsByTrafficStatus, podsToLabel)
	}

	var achievedPercentage float64
	if podsInApp == 0 {
		achievedPercentage = 0
	} else {
		achievedPercentage = float64(podsReady) / float64(podsInApp)
	}
	achievedWeight := uint32(math.Round(achievedPercentage * float64(totalTargetWeight)))

	return trafficShiftingStatus{
		achievedTrafficWeight: achievedWeight,
		podsReady:             podsReady,
		podsNotReady:          podsNotReady,
		podsLabeled:           podsLabeledForTraffic,
		ready:                 ready,
		podsToShift:           podsToShift,
	}
}

// buildPodsToShift returns a map of which label has to applied to which pods
// so we have the correct amount of pods labeled to receive traffic.
func buildPodsToShift(
	podsByTrafficStatus map[string][]*corev1.Pod,
	podsToLabel int,
) map[string][]*corev1.Pod {
	var oldStatus, newStatus string
	var podsToTake int

	podsLabeledForTraffic := len(podsByTrafficStatus[shipper.Enabled])
	if podsLabeledForTraffic > podsToLabel {
		// If we have more pods labeled for traffic than we
		// need, we'll change some pods with
		// PodTrafficStatusLabel=Enabled to Disabled...
		podsToTake = podsLabeledForTraffic - podsToLabel
		oldStatus = shipper.Enabled
		newStatus = shipper.Disabled
	} else {
		// ... or some pods with PodTrafficStatusLabel=Disabled
		// to Enabled otherwise.
		podsToTake = podsToLabel - podsLabeledForTraffic
		oldStatus = shipper.Disabled
		newStatus = shipper.Enabled
	}

	if podsToTake > len(podsByTrafficStatus[oldStatus]) {
		podsToTake = len(podsByTrafficStatus[oldStatus])
	}

	if podsToTake > 0 {
		return map[string][]*corev1.Pod{
			newStatus: podsByTrafficStatus[oldStatus][:podsToTake],
		}
	}

	return nil
}

// summarizePods returns an aggregated summary of the current state of pods:
// which pods are labeled to receive (or not receive) traffic, how many belong
// to the specified release, and how many are ready according to the Endpoints
// object.
func summarizePods(
	pods []*corev1.Pod,
	endpoints *corev1.Endpoints,
	releaseSelector labels.Selector,
) (map[string][]*corev1.Pod, int, int, int) {
	podsInRelease := make(map[string]struct{})
	podsByTrafficStatus := make(map[string][]*corev1.Pod)

	sort.Slice(pods, func(i, j int) bool {
		return pods[i].Name < pods[j].Name
	})

	for _, pod := range pods {
		if !releaseSelector.Matches(labels.Set(pod.Labels)) {
			continue
		}

		podsInRelease[pod.Name] = struct{}{}

		v, ok := pod.Labels[shipper.PodTrafficStatusLabel]
		if !ok {
			v = shipper.Disabled
		}

		podsByTrafficStatus[v] = append(podsByTrafficStatus[v], pod)
	}

	podReadiness := make(map[string]bool)
	for _, subset := range endpoints.Subsets {
		markAddressReadiness(podReadiness, subset.Addresses, true)
		markAddressReadiness(podReadiness, subset.NotReadyAddresses, false)
	}

	podsReady := 0
	podsNotReady := 0
	for podName, podReady := range podReadiness {
		_, belongsToRelease := podsInRelease[podName]

		if !belongsToRelease {
			continue
		}

		if podReady {
			podsReady++
		} else {
			podsNotReady++
		}
	}

	return podsByTrafficStatus, len(podsInRelease), podsReady, podsNotReady
}

// markAddressReadiness updates podReadiness  by marking
// the pods from a list of EndpointAddress as either ready or not ready
// according to the markAs parameter.
func markAddressReadiness(
	podReadiness map[string]bool,
	addresses []corev1.EndpointAddress,
	markAs bool,
) {
	for _, address := range addresses {
		target := address.TargetRef
		// Don't know what to do if the target is not a Pod, so
		// just skip it.
		if target.Kind != "Pod" {
			continue
		}

		podReadiness[target.Name] = markAs
	}
}

/*
	Transform this (a list of each release's traffic target object in this namespace):
	[
		{ tt-reviewsapi-1: { cluster-1: 90 } },
		{ tt-reviewsapi-2: { cluster-1: 5 } },
		{ tt-reviewsapi-3: { cluster-1: 5 } },
	]

	Into this (a map of release weight per cluster):
	{
		cluster-1: {
			reviewsapi-1: 90,
			reviewsapi-2: 5,
			reviewsapi-3: 5,
		}
	}
*/
func buildClusterReleaseWeights(trafficTargets []*shipper.TrafficTarget) (clusterReleaseWeights, error) {
	clusterReleases := map[string]map[string]uint32{}
	releaseTT := map[string]*shipper.TrafficTarget{}

	for _, tt := range trafficTargets {
		release, ok := tt.Labels[shipper.ReleaseLabel]
		if !ok {
			err := shippererrors.NewMissingShipperLabelError(tt, shipper.ReleaseLabel)
			return nil, err
		}

		existingTT, ok := releaseTT[release]
		if ok {
			return nil, shippererrors.NewMultipleTrafficTargetsForReleaseError(
				tt.Namespace, release, []string{tt.Name, existingTT.Name})
		}
		releaseTT[release] = tt

		for _, cluster := range tt.Spec.Clusters {
			weights, ok := clusterReleases[cluster.Name]
			if !ok {
				weights = map[string]uint32{}
				clusterReleases[cluster.Name] = weights
			}
			weights[release] += cluster.Weight
		}
	}

	return clusterReleaseWeights(clusterReleases), nil
}

func calculateReleasePodTarget(releasePods int, releaseWeight uint32, totalPods int, totalWeight uint32) int {
	// What percentage of the entire fleet (across all releases) should
	// this set of pods represent.
	var targetPercent float64
	if totalWeight == 0 {
		targetPercent = 0
	} else {
		targetPercent = float64(releaseWeight) / float64(totalWeight) * 100
	}

	// Round up to the nearest pod, clamped to the number of pods this
	// release has.
	targetPods := int(replicas.CalculateDesiredReplicaCount(int32(totalPods), int32(targetPercent)))
	targetPods = int(math.Min(float64(releasePods), float64(targetPods)))

	return targetPods
}
