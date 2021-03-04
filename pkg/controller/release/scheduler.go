package release

import (
	gocontext "context"
	"fmt"
	"sort"
	"strings"

	helmchart "helm.sh/helm/v3/pkg/chart"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperchart "github.com/bookingcom/shipper/pkg/chart"
	shipperrepo "github.com/bookingcom/shipper/pkg/chart/repo"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/controller"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

type Scheduler struct {
	clientset shipperclientset.Interface

	clusterLister            listers.ClusterLister
	installationTargetLister listers.InstallationTargetLister
	trafficTargetLister      listers.TrafficTargetLister
	capacityTargetLister     listers.CapacityTargetLister
	rolloutBlockLister       listers.RolloutBlockLister

	chartFetcher shipperrepo.ChartFetcher

	recorder record.EventRecorder
}

func NewScheduler(
	clientset shipperclientset.Interface,
	clusterLister listers.ClusterLister,
	installationTargerLister listers.InstallationTargetLister,
	capacityTargetLister listers.CapacityTargetLister,
	trafficTargetLister listers.TrafficTargetLister,
	rolloutBlockLister listers.RolloutBlockLister,
	chartFetcher shipperrepo.ChartFetcher,
	recorder record.EventRecorder,
) *Scheduler {
	return &Scheduler{
		clientset: clientset,

		clusterLister:            clusterLister,
		installationTargetLister: installationTargerLister,
		trafficTargetLister:      trafficTargetLister,
		capacityTargetLister:     capacityTargetLister,
		rolloutBlockLister:       rolloutBlockLister,

		chartFetcher: chartFetcher,

		recorder: recorder,
	}
}

func (s *Scheduler) ChooseClusters(rel *shipper.Release) (*shipper.Release, error) {
	metaKey := controller.MetaKey(rel)
	if releaseHasClusters(rel) {
		return nil, shippererrors.NewUnrecoverableError(fmt.Errorf("release %q has already been assigned to clusters", metaKey))
	}
	klog.V(4).Infof("Choosing clusters for release %q", metaKey)

	selector := labels.Everything()
	allClusters, err := s.clusterLister.List(selector)
	if err != nil {
		return nil, shippererrors.NewKubeclientListError(
			shipper.SchemeGroupVersion.WithKind("Cluster"),
			"", selector, err)
	}

	selectedClusters, err := computeTargetClusters(rel, allClusters)
	if err != nil {
		return nil, err
	}
	setReleaseClusters(rel, selectedClusters)

	return rel, nil
}

func (s *Scheduler) ScheduleRelease(rel *shipper.Release) (*releaseInfo, error) {
	metaKey := controller.MetaKey(rel)
	klog.V(4).Infof("Processing release %q", metaKey)
	defer klog.V(4).Infof("Finished processing %q", metaKey)

	if !releaseHasClusters(rel) {
		var err error
		rel, err = s.ChooseClusters(rel)
		if err != nil {
			return nil, err
		}
		s.recorder.Eventf(
			rel,
			corev1.EventTypeNormal,
			"ClustersSelected",
			"Set clusters for %q to %v",
			controller.MetaKey(rel),
			rel.Annotations[shipper.ReleaseClustersAnnotation],
		)
	}

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

func releaseHasClusters(rel *shipper.Release) bool {
	return len(rel.Annotations[shipper.ReleaseClustersAnnotation]) > 0
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
			klog.Errorf("Failed to get InstallationTarget %q from lister interface: %s",
				controller.MetaKey(rel),
				err)
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
				Chart:       rel.Spec.Environment.Chart.DeepCopy(),
				Values:      rel.Spec.Environment.Values,
				CanOverride: true,
			},
		}
		setInstallationTargetClusters(it, clusters)

		updIt, err := s.clientset.ShipperV1alpha1().InstallationTargets(rel.GetNamespace()).Create(gocontext.TODO(),it, metav1.CreateOptions{})
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
		klog.V(4).Infof("Updating InstallationTarget %q clusters to %s",
			controller.MetaKey(it),
			strings.Join(clusters, ","))
		setInstallationTargetClusters(it, clusters)
		updIt, err := s.clientset.ShipperV1alpha1().InstallationTargets(rel.GetNamespace()).Update(gocontext.TODO(), it, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("Failed to update InstallationTarget %q clusters: %s",
				controller.MetaKey(it),
				err)
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
			klog.Errorf("Failed to get CapacityTarget %q from lister interface: %s",
				controller.MetaKey(rel),
				err)
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

		updCt, err := s.clientset.ShipperV1alpha1().CapacityTargets(rel.GetNamespace()).Create(gocontext.TODO(), ct, metav1.CreateOptions{})
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
		klog.V(4).Infof("Updating CapacityTarget %q clusters to %s",
			controller.MetaKey(ct),
			strings.Join(clusters, ","))
		setCapacityTargetClusters(ct, clusters, totalReplicaCount)
		updCt, err := s.clientset.ShipperV1alpha1().CapacityTargets(rel.GetNamespace()).Update(gocontext.TODO(), ct, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("Failed to update CapacityTarget %q clusters: %s",
				controller.MetaKey(ct),
				err)
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
			klog.Errorf("Failed to get TrafficTarget %q from lister interface: %s",
				controller.MetaKey(rel),
				err)
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

		updTt, err := s.clientset.ShipperV1alpha1().TrafficTargets(rel.GetNamespace()).Create(gocontext.TODO(), tt, metav1.CreateOptions{})
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
		klog.V(4).Infof("Updating TrafficTarget %q clusters to %s",
			controller.MetaKey(tt),
			strings.Join(clusters, ","))
		setTrafficTargetClusters(tt, clusters)
		updTt, err := s.clientset.ShipperV1alpha1().TrafficTargets(rel.GetNamespace()).Update(gocontext.TODO(), tt, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("Failed to update TrafficTarget %q clusters: %s",
				controller.MetaKey(tt),
				err)
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

// computeTargetClusters picks out the clusters from the given list which match
// the release's clusterRequirements.
func computeTargetClusters(rel *shipper.Release, clusterList []*shipper.Cluster) ([]*shipper.Cluster, error) {
	regionSpecs := rel.Spec.Environment.ClusterRequirements.Regions
	requiredCapabilities := rel.Spec.Environment.ClusterRequirements.Capabilities
	capableClustersByRegion := map[string][]*shipper.Cluster{}
	regionReplicas := map[string]int{}

	if len(regionSpecs) == 0 {
		return nil, shippererrors.NewNoRegionsSpecifiedError()
	}

	app, err := releaseutil.ApplicationNameForRelease(rel)
	if err != nil {
		return nil, err
	}

	err = validateClusterRequirements(rel.Spec.Environment.ClusterRequirements)
	if err != nil {
		return nil, err
	}

	prefList := buildPrefList(app, clusterList)
	// This algo could probably build up hashes instead of doing linear searches,
	// but these data sets are so tiny (1-20 items) that it'd only be useful for
	// readability.
	for _, region := range regionSpecs {
		capableClustersByRegion[region.Name] = []*shipper.Cluster{}
		if region.Replicas == nil {
			regionReplicas[region.Name] = 1
		} else {
			regionReplicas[region.Name] = int(*region.Replicas)
		}

		matchedRegion := 0
		for _, cluster := range prefList {
			if cluster.Spec.Scheduler.Unschedulable {
				continue
			}

			if cluster.Spec.Region == region.Name {
				matchedRegion++
				capabilityMatch := 0
				for _, requiredCapability := range requiredCapabilities {
					for _, providedCapability := range cluster.Spec.Capabilities {
						if requiredCapability == providedCapability {
							capabilityMatch++
							break
						}
					}
				}

				if capabilityMatch == len(requiredCapabilities) {
					capableClustersByRegion[region.Name] = append(capableClustersByRegion[region.Name], cluster)
				}
			}
		}
		if regionReplicas[region.Name] > matchedRegion {
			return nil, shippererrors.NewNotEnoughClustersInRegionError(region.Name, regionReplicas[region.Name], matchedRegion)
		}
	}

	resClusters := make([]*shipper.Cluster, 0)
	for region, clusters := range capableClustersByRegion {
		if regionReplicas[region] > len(clusters) {
			return nil, shippererrors.NewNotEnoughCapableClustersInRegionError(
				region,
				requiredCapabilities,
				regionReplicas[region],
				len(clusters),
			)
		}

		//NOTE(btyler): this assumes we do not have duplicate cluster names. For the
		//moment cluster objects are cluster scoped; if they become namespace scoped
		//and releases can somehow be scheduled to clusters from multiple namespaces,
		//this assumption will be wrong.
		for _, cluster := range clusters {
			if regionReplicas[region] > 0 {
				regionReplicas[region]--
				resClusters = append(resClusters, cluster)
			}
		}
	}

	sort.Slice(resClusters, func(i, j int) bool {
		return resClusters[i].Name < resClusters[j].Name
	})

	return resClusters, nil
}

func validateClusterRequirements(requirements shipper.ClusterRequirements) error {
	// Ensure capability uniqueness. Erroring instead of de-duping in order to
	// avoid second-guessing by operators about how Shipper might treat repeated
	// listings of the same capability.
	seenCapabilities := map[string]struct{}{}
	for _, capability := range requirements.Capabilities {
		_, ok := seenCapabilities[capability]
		if ok {
			return shippererrors.NewDuplicateCapabilityRequirementError(capability)
		}
		seenCapabilities[capability] = struct{}{}
	}

	return nil
}

func setReleaseClusters(rel *shipper.Release, clusters []*shipper.Cluster) {
	clusterNames := make([]string, 0, len(clusters))
	memo := make(map[string]struct{})
	for _, cluster := range clusters {
		if _, ok := memo[cluster.Name]; ok {
			continue
		}
		clusterNames = append(clusterNames, cluster.Name)
		memo[cluster.Name] = struct{}{}
	}
	sort.Strings(clusterNames)
	rel.Annotations[shipper.ReleaseClustersAnnotation] = strings.Join(clusterNames, ",")
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

	klog.V(4).Infof("Extracted %d replicas from release %q", replicas, controller.MetaKey(rel))

	return int32(replicas), nil
}

func extractReplicasFromChartForRel(chart *helmchart.Chart, rel *shipper.Release) (int32, error) {
	owners := rel.OwnerReferences
	if l := len(owners); l != 1 {
		return 0, shippererrors.NewMultipleOwnerReferencesError(rel.Name, l)
	}

	applicationName := owners[0].Name
	rendered, err := shipperchart.Render(chart, applicationName, rel.Namespace, rel.Spec.Environment.Values)
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
