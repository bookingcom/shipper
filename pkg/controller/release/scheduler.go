package release

import (
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
	"github.com/bookingcom/shipper/pkg/controller"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

type Scheduler struct {
	// TODO(jgreff): explain why we need this
	clientset           shipperclientset.Interface
	appClusterClientset shipperclientset.Interface

	listers listers

	chartFetcher shipperrepo.ChartFetcher

	recorder record.EventRecorder
}

func NewScheduler(
	clientset shipperclientset.Interface,
	appClusterClientset shipperclientset.Interface,
	listers listers,
	chartFetcher shipperrepo.ChartFetcher,
	recorder record.EventRecorder,
) *Scheduler {
	return &Scheduler{
		clientset:           clientset,
		appClusterClientset: appClusterClientset,

		listers: listers,

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

// The 2 functions below are based on a basic cluster name set match, and never
// take into account a cluster weight change. This must be addressed in the
// future.
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
	it, err := s.listers.installationTargetLister.InstallationTargets(rel.GetNamespace()).Get(rel.GetName())
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}

		it := &shipper.InstallationTarget{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rel.Name,
				Namespace: rel.Namespace,
				Labels:    rel.Labels,
				// OwnerReferences: []metav1.OwnerReference{
				// 	createOwnerRefFromRelease(rel),
				// },
			},
			Spec: shipper.InstallationTargetSpec{
				Chart:       rel.Spec.Environment.Chart,
				Values:      rel.Spec.Environment.Values,
				CanOverride: true,
			},
		}

		updIt, err := s.appClusterClientset.ShipperV1alpha1().InstallationTargets(rel.GetNamespace()).Create(it)
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

	// TODO(jgreff): ensure things still make sense in this case
	// if !objectBelongsToRelease(it, rel) {
	// 	return nil, shippererrors.NewWrongOwnerReferenceError(it, rel)
	// }

	// TODO(jgreff): ensure InstallationTargets are inserted and removed from clusters correctly
	// if !installationTargetClustersMatch(it, clusters) {
	// 	klog.V(4).Infof("Updating InstallationTarget %q clusters to %s",
	// 		controller.MetaKey(it),
	// 		strings.Join(clusters, ","))
	// 	setInstallationTargetClusters(it, clusters)
	// 	updIt, err := s.clientset.ShipperV1alpha1().InstallationTargets(rel.GetNamespace()).Update(it)
	// 	if err != nil {
	// 		klog.Errorf("Failed to update InstallationTarget %q clusters: %s",
	// 			controller.MetaKey(it),
	// 			err)
	// 		return nil, err
	// 	}
	// 	s.recorder.Eventf(
	// 		rel,
	// 		corev1.EventTypeNormal,
	// 		"ReleaseScheduled",
	// 		"Updated InstallationTarget %q cluster set to [%s]",
	// 		controller.MetaKey(updIt),
	// 		strings.Join(clusters, ","))
	// 	return updIt, nil
	// }

	return it, nil
}

func (s *Scheduler) CreateOrUpdateCapacityTarget(rel *shipper.Release, totalReplicaCount int32) (*shipper.CapacityTarget, error) {
	clusters := getReleaseClusters(rel)

	ct, err := s.listers.capacityTargetLister.CapacityTargets(rel.GetNamespace()).Get(rel.GetName())
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

	tt, err := s.listers.trafficTargetLister.TrafficTargets(rel.GetNamespace()).Get(rel.GetName())
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
	rendered, err := shipperchart.Render(
		chart,
		applicationName,
		rel.Namespace,
		&rel.Spec.Environment.Values)

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
