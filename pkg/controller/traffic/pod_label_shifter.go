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
	releaseName           string
	namespace             string
	clusterReleaseWeights clusterReleaseWeights
}

type clusterReleaseWeights map[string]map[string]uint32

func newPodLabelShifter(
	appName string,
	releaseName string,
	namespace string,
	trafficTargets []*shipper.TrafficTarget,
) (*podLabelShifter, error) {
	weights, err := buildClusterReleaseWeights(trafficTargets)
	if err != nil {
		return nil, err
	}

	return &podLabelShifter{
		appName:               appName,
		releaseName:           releaseName,
		namespace:             namespace,
		clusterReleaseWeights: weights,
	}, nil
}

func (p *podLabelShifter) SyncCluster(
	cluster string,
	clientset kubernetes.Interface,
	informerFactory kubeinformers.SharedInformerFactory,
) (uint32, error) {
	releaseTargetWeights, ok := p.clusterReleaseWeights[cluster]
	if !ok {
		return 0, shippererrors.NewMissingTrafficWeightsForClusterError(
			p.namespace, p.appName, cluster)
	}

	podLister := informerFactory.Core().V1().Pods().Lister().Pods(p.namespace)
	podGVK := corev1.SchemeGroupVersion.WithKind("Pod")

	appSelector := labels.Set{shipper.AppLabel: p.appName}.AsSelector()
	appPods, err := podLister.List(appSelector)
	if err != nil {
		return 0, shippererrors.NewKubeclientListError(
			podGVK, p.namespace, appSelector, err)
	}

	releaseSelector := labels.Set{shipper.ReleaseLabel: p.releaseName}.AsSelector()

	svc, err := p.getService(informerFactory)
	if err != nil {
		return 0, err
	} else if svc.Spec.Selector == nil {
		return 0, shippererrors.NewTargetClusterServiceMissesSelectorError(p.namespace, svc.Name)
	}

	trafficSelector := labels.Set(svc.Spec.Selector).AsSelector()

	endpoints, err := informerFactory.Core().V1().Endpoints().Lister().
		Endpoints(svc.Namespace).Get(svc.Name)
	if err != nil {
		return 0, shippererrors.NewKubeclientGetError(svc.Namespace, svc.Name, err).
			WithCoreV1Kind("Endpoints")
	}

	status := NewTrafficShiftingStatus(appPods, endpoints, trafficSelector, releaseSelector)

	var totalTargetWeight uint32 = 0
	for _, weight := range releaseTargetWeights {
		totalTargetWeight += weight
	}

	releaseTargetWeight := releaseTargetWeights[p.releaseName]
	targetPods := calculateReleasePodTarget(
		status.ReleasePods, releaseTargetWeight, len(appPods), totalTargetWeight)

	// TODO(jgreff): we need to be able to tell, from the outside, that all
	// pods have been labeled, but some may not be ready yet.

	if len(status.LabeledPods) != targetPods {
		err = p.shiftLabels(status, targetPods, clientset)
		if err != nil {
			return 0, err
		}

		achievedWeight := uint32(round(status.AchievedPercentage * float64(totalTargetWeight)))
		return achievedWeight, nil
	} else {
		return releaseTargetWeight, nil
	}
}

func (p *podLabelShifter) shiftLabels(
	status *TrafficShiftingStatus,
	targetPods int,
	clientset kubernetes.Interface,
) error {
	patches := make(map[string][]byte)

	if len(status.LabeledPods) > targetPods {
		excess := len(status.LabeledPods) - targetPods

		for i := 0; i < excess; i++ {
			pod := status.LabeledPods[i].DeepCopy()

			value, ok := pod.Labels[shipper.PodTrafficStatusLabel]
			if !ok || value == shipper.Enabled {
				patches[pod.Name] = patchPodTrafficStatusLabel(pod, shipper.Disabled)
			}
		}
	} else {
		missing := targetPods - len(status.LabeledPods)

		if missing > len(status.UnlabeledPods) {
			return shippererrors.NewTargetClusterMathError(p.releaseName, len(status.UnlabeledPods), missing)
		}

		for i := 0; i < missing; i++ {
			pod := status.UnlabeledPods[i].DeepCopy()

			value, ok := pod.Labels[shipper.PodTrafficStatusLabel]
			if !ok || ok && value == shipper.Disabled {
				patches[pod.Name] = patchPodTrafficStatusLabel(pod, shipper.Enabled)
			}
		}
	}

	podsClient := clientset.CoreV1().Pods(p.namespace)
	for podName, patch := range patches {
		_, err := podsClient.Patch(podName, types.JSONPatchType, patch)
		if err != nil {
			return shippererrors.
				NewKubeclientPatchError(p.namespace, podName, err).
				WithCoreV1Kind("Pod")
		}
	}

	return nil
}

func (p *podLabelShifter) getService(
	informerFactory kubeinformers.SharedInformerFactory,
) (*corev1.Service, error) {
	serviceSelector := labels.Set(map[string]string{
		shipper.AppLabel: p.appName,
		shipper.LBLabel:  shipper.LBForProduction,
	}).AsSelector()

	gvk := corev1.SchemeGroupVersion.WithKind("Service")
	services, err := informerFactory.Core().V1().Services().Lister().
		Services(p.namespace).List(serviceSelector)
	if err != nil {
		return nil, shippererrors.NewKubeclientListError(
			gvk, p.namespace, serviceSelector, err)
	}

	if n := len(services); n != 1 {
		return nil, shippererrors.NewUnexpectedObjectCountFromSelectorError(
			serviceSelector, gvk, 1, n)
	}

	return services[0], nil
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
