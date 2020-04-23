package release

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	helmchart "k8s.io/helm/pkg/proto/hapi/chart"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperchart "github.com/bookingcom/shipper/pkg/chart"
	shipperrepo "github.com/bookingcom/shipper/pkg/chart/repo"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	objectutil "github.com/bookingcom/shipper/pkg/util/object"
)

type Scheduler struct {
	clientset    shipperclientset.Interface
	listers      listers
	chartFetcher shipperrepo.ChartFetcher
	recorder     record.EventRecorder
}

func NewScheduler(
	clientset shipperclientset.Interface,
	listers listers,
	chartFetcher shipperrepo.ChartFetcher,
	recorder record.EventRecorder,
) *Scheduler {
	return &Scheduler{
		clientset:    clientset,
		listers:      listers,
		chartFetcher: chartFetcher,
		recorder:     recorder,
	}
}

func (s *Scheduler) ScheduleRelease(rel *shipper.Release) (*releaseInfo, error) {
	replicaCount, err := s.fetchChartAndExtractReplicaCount(rel)
	if err != nil {
		return nil, err
	}

	releaseErrors := shippererrors.NewMultiError()

	it, err := s.createInstallationTarget(rel)
	if err != nil {
		releaseErrors.Append(err)
	}

	tt, err := s.createTrafficTarget(rel)
	if err != nil {
		releaseErrors.Append(err)
	}

	ct, err := s.createCapacityTarget(rel, replicaCount)
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

func (s *Scheduler) createInstallationTarget(rel *shipper.Release) (*shipper.InstallationTarget, error) {
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
			},
			Spec: shipper.InstallationTargetSpec{
				Chart:       rel.Spec.Environment.Chart,
				Values:      rel.Spec.Environment.Values,
				CanOverride: true,
			},
		}

		updIt, err := s.clientset.ShipperV1alpha1().InstallationTargets(rel.GetNamespace()).Create(it)
		if err != nil {
			return nil, shippererrors.NewKubeclientCreateError(it, err)
		}

		s.recorder.Eventf(
			rel,
			corev1.EventTypeNormal,
			"ReleaseScheduled",
			"Created InstallationTarget %q",
			objectutil.MetaKey(updIt),
		)

		return updIt, nil
	}

	return it, nil
}

func (s *Scheduler) createCapacityTarget(rel *shipper.Release, totalReplicaCount int32) (*shipper.CapacityTarget, error) {
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
			},
			Spec: shipper.CapacityTargetSpec{
				TotalReplicaCount: totalReplicaCount,
			},
		}

		updCt, err := s.clientset.ShipperV1alpha1().CapacityTargets(rel.GetNamespace()).Create(ct)
		if err != nil {
			return nil, shippererrors.NewKubeclientCreateError(ct, err)
		}

		s.recorder.Eventf(
			rel,
			corev1.EventTypeNormal,
			"ReleaseScheduled",
			"Created CapacityTarget %q",
			objectutil.MetaKey(updCt),
		)

		return updCt, nil
	}

	return ct, nil
}

func (s *Scheduler) createTrafficTarget(rel *shipper.Release) (*shipper.TrafficTarget, error) {
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
			},
		}

		updTt, err := s.clientset.ShipperV1alpha1().TrafficTargets(rel.GetNamespace()).Create(tt)
		if err != nil {
			return nil, shippererrors.NewKubeclientCreateError(tt, err)
		}

		s.recorder.Eventf(
			rel,
			corev1.EventTypeNormal,
			"ReleaseScheduled",
			"Created TrafficTarget %q",
			objectutil.MetaKey(updTt),
		)

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
	applicationName, err := objectutil.GetApplicationLabel(rel)
	if err != nil {
		return 0, err
	}

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
