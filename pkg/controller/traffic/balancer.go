package traffic

import (
	"fmt"
	"math"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	corev1informer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

type balancer struct {
	namespace             string
	clusterReleaseWeights clusterReleaseWeights
}

type clusterReleaseWeights map[string]map[string]int

func newBalancer(namespace string, trafficTargets []*shipperv1.TrafficTarget) (*balancer, error) {
	weights, err := buildClusterReleaseWeights(trafficTargets)
	if err != nil {
		return nil, err
	}
	return &balancer{
		namespace:             namespace,
		clusterReleaseWeights: weights,
	}, nil
}

func (b *balancer) Clusters() []string {
	clusters := make([]string, 0, len(b.clusterReleaseWeights))
	for cluster, _ := range b.clusterReleaseWeights {
		clusters = append(clusters, cluster)
	}
	sort.Strings(clusters)
	return clusters
}

func (b *balancer) syncCluster(cluster string, clientset kubernetes.Interface, informer corev1informer.PodInformer) []error {
	releaseWeights, ok := b.clusterReleaseWeights[cluster]
	if !ok {
		return []error{fmt.Errorf("balancer has no weights for cluster %q")}
	}

	podsClient := clientset.CoreV1().Pods(b.namespace)
	servicesClient := clientset.CoreV1().Services(b.namespace)

	// NOTE(btyler) namespace == app name == service object name here
	svcName := b.namespace
	prodSvc, err := servicesClient.Get(fmt.Sprintf("%s-prod", svcName), metav1.GetOptions{})
	if err != nil {
		return []error{fmt.Errorf(
			"failed to fetch prod service %s-prod in '%s/%s': %q",
			svcName, cluster, b.namespace, err,
		)}
	}

	trafficSelector := prodSvc.Spec.Selector
	if trafficSelector == nil {
		return []error{fmt.Errorf(
			"cluster error (%q): service %s/%s does not have a selector set. this means we cannot do label-based canary deployment",
			cluster, b.namespace, svcName,
		)}
	}

	nsPodLister := informer.Lister().Pods(b.namespace)

	// NOTE(btyler) namespace == one app (because we're fetching all the pods in the ns)
	pods, err := nsPodLister.List(labels.Everything())
	if err != nil {
		return []error{fmt.Errorf(
			"cluster error (%q): failed to list pods in '%s': %q",
			cluster, b.namespace, err,
		)}
	}

	totalPods := len(pods)
	totalWeight := 0
	for _, weight := range releaseWeights {
		totalWeight += weight
	}

	errors := []error{}
	for release, weight := range releaseWeights {
		releaseReq, err := labels.NewRequirement("release", selection.Equals, []string{release})
		if err != nil {
			// programmer error: this is a static label
			panic(err)
		}

		releaseSelector := labels.NewSelector().Add(*releaseReq)
		releasePods, err := nsPodLister.List(releaseSelector)
		if err != nil {
			errors = append(errors, fmt.Errorf(
				"release error (%q): failed to list pods in '%s/%s': %q",
				release, cluster, b.namespace, err,
			))
			continue
		}

		targetPods := calculateReleasePodTarget(len(releasePods), weight, totalPods, totalWeight)

		trafficPods := []*corev1.Pod{}
		idlePods := []*corev1.Pod{}
		for _, pod := range releasePods {
			if getsTraffic(pod, trafficSelector) {
				trafficPods = append(trafficPods, pod)
				continue
			}
			idlePods = append(idlePods, pod)
		}

		// everything is fine, nothing to do
		if len(trafficPods) == targetPods {
			continue
		}

		if len(trafficPods) > targetPods {
			excess := len(trafficPods) - targetPods
			for i := 0; i < excess; i++ {
				pod := trafficPods[i].DeepCopy()

				removeFromLB(pod, trafficSelector)

				_, err := podsClient.Update(pod)
				if err != nil {
					errors = append(errors, fmt.Errorf(
						"pod error (%s/%s/%s): failed to add traffic label: %q",
						cluster, b.namespace, pod.Name, err,
					))
					continue
				}
			}
		}

		if len(trafficPods) < targetPods {
			missing := targetPods - len(trafficPods)
			if missing > len(idlePods) {
				errors = append(errors, fmt.Errorf(
					"release error (%q): the math is broken: there aren't enough idle pods (%d) to meet requested increase in traffic pods (%d).",
					release, len(idlePods), missing,
				))
				continue
			}

			for i := 0; i < missing; i++ {
				pod := idlePods[i].DeepCopy()

				addToLB(pod, trafficSelector)

				_, err := podsClient.Update(pod)
				if err != nil {
					errors = append(errors, fmt.Errorf(
						"pod error (%s/%s/%s): failed to add traffic label: %q",
						cluster, b.namespace, pod.Name, err,
					))
					continue
				}
			}
		}
	}

	// check that service obj is in place and has the right bits
	// query for pods for each release that we had a TT for, hang on to them
	//
	//releasePodCounts := map[string]uint{}
	return errors
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

func addToLB(pod *corev1.Pod, trafficSelector map[string]string) {
	for key, trafficValue := range trafficSelector {
		pod.Labels[key] = trafficValue
	}
}

// NOTE(btyler) there's probably a case to make about not deleting the label
// entirely, but just changing it. however, without a known alternate value to
// change to I think deletion is the only reasonable approach
func removeFromLB(pod *corev1.Pod, trafficSelector map[string]string) {
	for key, _ := range trafficSelector {
		delete(pod.Labels, key)
	}
}

func calculateReleasePodTarget(releasePods, releaseWeight, totalPods, totalWeight int) int {
	// what percentage of the entire fleet (across all releases) should this set of pods represent
	var targetPercent float64
	if totalWeight == 0 {
		targetPercent = 0
	} else {
		targetPercent = float64(releaseWeight) / float64(totalWeight)
	}
	// round up to the nearest pod, clamped to the number of pods this release has
	targetPods := int(
		math.Min(
			float64(releasePods),
			math.Ceil(targetPercent*float64(totalPods)),
		),
	)
	return targetPods
}

/*
	transform this (a list of each release's traffic target object in this namespace):
	[
		{ tt-reviewsapi-1: { cluster-1: 90 } },
		{ tt-reviewsapi-2: { cluster-1: 5 } },
		{ tt-reviewsapi-3: { cluster-1: 5 } },
	]

	into this (a map of release weight per cluster):
	{
		cluster-1: {
			reviewsapi-1: 90,
			reviewsapi-2: 5,
			reviewsapi-3: 5,
		}
	}
*/
func buildClusterReleaseWeights(trafficTargets []*shipperv1.TrafficTarget) (clusterReleaseWeights, error) {
	clusterReleases := map[string]map[string]int{}
	releaseTT := map[string]*shipperv1.TrafficTarget{}
	for _, tt := range trafficTargets {
		release, ok := tt.Labels["release"]
		if !ok {
			return nil, fmt.Errorf(
				"TrafficTarget '%s/%s' needs a 'release' label in order to select resources in the target clusters.",
				tt.Namespace, tt.Name,
			)
		}
		existingTT, ok := releaseTT[release]
		if ok {
			return nil, fmt.Errorf(
				"TrafficTargets %q and %q in namespace %q both operate on release %q. This is wrong, please fix",
				existingTT.Name, tt.Name, tt.Namespace, release,
			)
		}
		releaseTT[release] = tt

		for _, cluster := range tt.Spec.Clusters {
			weights, ok := clusterReleases[cluster.Name]
			if !ok {
				weights = map[string]int{}
				clusterReleases[cluster.Name] = weights
			}
			weights[release] += int(cluster.TargetTraffic)
		}
	}
	return clusterReleaseWeights(clusterReleases), nil
}
