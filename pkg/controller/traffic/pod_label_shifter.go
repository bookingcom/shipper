package traffic

import (
	"encoding/json"
	"fmt"
	"math"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	"github.com/bookingcom/shipper/pkg/util/replicas"
)

type podLabelShifter struct {
	appName               string
	namespace             string
	serviceSelector       labels.Selector
	clusterReleaseWeights clusterReleaseWeights
}

type clusterReleaseWeights map[string]map[string]uint32

func newPodLabelShifter(
	appName string,
	namespace string,
	trafficTargets []*shipper.TrafficTarget,
) (*podLabelShifter, error) {

	weights, err := buildClusterReleaseWeights(trafficTargets)
	if err != nil {
		return nil, err
	}

	serviceSelector := map[string]string{
		shipper.AppLabel: appName,
		shipper.LBLabel:  shipper.LBForProduction,
	}

	return &podLabelShifter{
		appName:               appName,
		namespace:             namespace,
		serviceSelector:       labels.Set(serviceSelector).AsSelector(),
		clusterReleaseWeights: weights,
	}, nil
}

func (p *podLabelShifter) SyncCluster(
	cluster string,
	release string,
	clientset kubernetes.Interface,
	informerFactory kubeinformers.SharedInformerFactory,
) (uint32, error) {
	releaseWeights, ok := p.clusterReleaseWeights[cluster]
	if !ok {
		return 0, shippererrors.NewMissingTrafficWeightsForClusterError(
			p.namespace, p.appName, cluster)
	}

	appSelector := labels.Set{shipper.AppLabel: p.appName}.AsSelector()
	releaseSelector := labels.Set{shipper.ReleaseLabel: release}.AsSelector()
	trafficSelector, err := p.getTrafficSelector(cluster, informerFactory)
	if err != nil {
		return 0, err
	}

	podLister := informerFactory.Core().V1().Pods().Lister().Pods(p.namespace)
	podGVK := corev1.SchemeGroupVersion.WithKind("Pod")

	appPods, err := podLister.List(appSelector)
	if err != nil {
		return 0, shippererrors.NewKubeclientListError(
			podGVK, p.namespace, appSelector, err)
	}

	releasePods, err := podLister.List(releaseSelector)
	if err != nil {
		return 0, shippererrors.NewKubeclientListError(
			podGVK, p.namespace, releaseSelector, err)
	}

	var totalWeight uint32 = 0
	for _, weight := range releaseWeights {
		totalWeight += weight
	}

	targetWeight := releaseWeights[release]
	targetPods := calculateReleasePodTarget(
		len(releasePods), targetWeight, len(appPods), totalWeight)

	var trafficPods []*corev1.Pod
	var idlePods []*corev1.Pod
	for _, pod := range releasePods {
		if getsTraffic(pod, trafficSelector) {
			trafficPods = append(trafficPods, pod)
			continue
		}
		idlePods = append(idlePods, pod)
	}

	// traffic achieved, no patches to apply.
	if len(trafficPods) == targetPods {
		return targetWeight, nil
	}

	var delta int
	patches := make(map[string][]byte)

	if len(trafficPods) > targetPods {
		excess := len(trafficPods) - targetPods

		for i := 0; i < excess; i++ {
			pod := trafficPods[i].DeepCopy()

			value, ok := pod.Labels[shipper.PodTrafficStatusLabel]
			if !ok || value == shipper.Enabled {
				patches[pod.Name] = patchPodTrafficStatusLabel(pod, shipper.Disabled)
			}

			delta--
		}
	} else {
		missing := targetPods - len(trafficPods)

		if missing > len(idlePods) {
			return 0, shippererrors.NewTargetClusterMathError(release, len(idlePods), missing)
		}

		for i := 0; i < missing; i++ {
			pod := idlePods[i].DeepCopy()

			value, ok := pod.Labels[shipper.PodTrafficStatusLabel]
			if !ok || ok && value == shipper.Disabled {
				patches[pod.Name] = patchPodTrafficStatusLabel(pod, shipper.Enabled)
			}

			delta++
		}
	}

	podsClient := clientset.CoreV1().Pods(p.namespace)
	for podName, patch := range patches {
		_, err := podsClient.Patch(podName, types.JSONPatchType, patch)
		if err != nil {
			return 0, shippererrors.
				NewKubeclientPatchError(p.namespace, podName, err).
				WithCoreV1Kind("Pod")
		}
	}

	// NOTE(jgreff): we assume that the patches will actually succeed in
	// making pods satisfy traffic requirements. that's the wrong thing to
	// do, and we should just return `len(trafficPods)`, and let the next
	// sync calculate what actually happened.
	finalTrafficPods := len(trafficPods) + delta
	proportion := float64(finalTrafficPods) / float64(len(appPods))
	achievedWeight := uint32(round(proportion * float64(totalWeight)))

	return achievedWeight, nil
}

func getsTraffic(pod *corev1.Pod, trafficSelectors map[string]string) bool {
	for key, trafficValue := range trafficSelectors {
		podValue, ok := pod.Labels[key]
		if !ok || podValue != trafficValue {
			return false
		}
	}
	return true
}

// PatchOperation represents a JSON PatchOperation in a very specific way.
// Using jsonpatch's types could be a possiblity, but there's no need to be
// generic in here.
type PatchOperation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value string `json:"value"`
}

// patchPodTrafficStatusLabel returns a JSON Patch that modifies the
// PodTrafficStatusLabel value of a given Pod.
func patchPodTrafficStatusLabel(pod *corev1.Pod, value string) []byte {
	var op string

	if _, ok := pod.Labels[shipper.PodTrafficStatusLabel]; ok {
		op = "replace"
	} else {
		op = "add"
	}

	patchList := []PatchOperation{
		{
			Op:    op,
			Path:  fmt.Sprintf("/metadata/labels/%s", shipper.PodTrafficStatusLabel),
			Value: value,
		},
	}

	// Don't know what to do in here. From my perspective it is quite
	// unlikely that the json.Marshal operation above would fail since its
	// input should be a valid serializable value.
	patchBytes, _ := json.Marshal(patchList)

	return patchBytes
}

func calculateReleasePodTarget(releasePods int, releaseWeight uint32, totalPods int, totalWeight uint32) int {
	// What percentage of the entire fleet (across all releases) should this set of
	// pods represent.
	var targetPercent float64
	if totalWeight == 0 {
		targetPercent = 0
	} else {
		targetPercent = float64(releaseWeight) / float64(totalWeight) * 100
	}
	// Round up to the nearest pod, clamped to the number of pods this release has.
	targetPods := int(replicas.CalculateDesiredReplicaCount(uint(totalPods), float64(targetPercent)))

	targetPods = int(
		math.Min(
			float64(releasePods),
			float64(targetPods),
		),
	)
	return targetPods
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

func round(num float64) int {
	if num < 0 {
		return int(num - 0.5)
	}
	return int(num + 0.5)
}

func (p *podLabelShifter) getTrafficSelector(
	cluster string,
	informerFactory kubeinformers.SharedInformerFactory,
) (map[string]string, error) {
	gvk := corev1.SchemeGroupVersion.WithKind("Service")
	services, err := informerFactory.Core().V1().Services().Lister().
		Services(p.namespace).List(p.serviceSelector)
	if err != nil {
		return nil, shippererrors.NewKubeclientListError(
			gvk, p.namespace, p.serviceSelector, err)
	}

	if n := len(services); n != 1 {
		return nil, shippererrors.NewUnexpectedObjectCountFromSelectorError(
			p.serviceSelector, gvk, 1, n)
	}

	prodSvc := services[0]
	trafficSelector := prodSvc.Spec.Selector
	if trafficSelector == nil {
		return nil, shippererrors.NewTargetClusterServiceMissesSelectorError(
			cluster, p.namespace, prodSvc.Name)
	}

	return trafficSelector, nil
}
