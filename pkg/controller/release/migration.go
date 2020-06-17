package release

import (
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	objectutil "github.com/bookingcom/shipper/pkg/util/object"
)

func (c *Controller) migrateTargetObjects(relName, namespace string) error {
	selector := labels.Set{shipper.ReleaseLabel: relName}.AsSelector()

	err := c.migrateCapacityTargets(relName, namespace, selector)
	if err != nil {
		return err
	}

	err = c.migrateTrafficTargets(relName, namespace, selector)
	if err != nil {
		return err
	}

	err = c.migrateInstallationTargets(relName, namespace, selector)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) migrateCapacityTargets(relName, namespace string, selector labels.Selector) error {
	gvk := shipper.SchemeGroupVersion.WithKind("CapacityTarget")
	// get target objects from mgmt cluster
	capacityTargets, err := c.capacityTargetLister.CapacityTargets(namespace).List(selector)
	if err != nil {
		if errors.IsNotFound(err) {
			// no capacityTargets for this release in mgmt cluster. moving on.
			return nil
		}
		return shippererrors.NewKubeclientListError(gvk, namespace, selector, err)
	}
	// there should be only one capacity target
	if len(capacityTargets) > 1 {
		return shippererrors.NewMultipleTargetObjectsForReleaseError(namespace, gvk.Kind, relName)
	}

	if len(capacityTargets) == 0 {
		// no objects to migrate
		return nil
	}

	// checking if we should migrate object
	initialCt := capacityTargets[0]
	migrationLabel, _ := objectutil.GetMigrationLabel(initialCt)
	if migrationLabel {
		// already migrated
		return nil
	}

	if initialCt.Spec.Clusters == nil || len(initialCt.Spec.Clusters) == 0 {
		// this capacity target doesn't have spec.clusters. moving on.
		return nil
	}

	klog.Infof("migrating capacity target for release %s/%s", namespace, relName)
	// create a new capacity target to put in application clusters
	ct := initialCt.DeepCopy()
	ct.Spec.Percent = initialCt.Spec.Clusters[0].Percent
	ct.Spec.TotalReplicaCount = initialCt.Spec.Clusters[0].TotalReplicaCount
	ct.ObjectMeta = metav1.ObjectMeta{
		Name:        initialCt.Name,
		Namespace:   initialCt.Namespace,
		Generation:  initialCt.Generation,
		Labels:      initialCt.Labels,
		Annotations: initialCt.Annotations,
	}

	// put in application clusters:
	for _, cluster := range initialCt.Spec.Clusters {
		clusterName := cluster.Name
		clusterClientsets, err := c.store.GetApplicationClusterClientset(clusterName, AgentName)
		if err != nil {
			return err
		}
		_, err = clusterClientsets.GetShipperClient().ShipperV1alpha1().CapacityTargets(ct.Namespace).Create(ct)
		if err == nil {
			// update initial object with migrated label
			initialCt.Annotations[shipper.MigrationAnnotation] = "true"
			_, err = c.clientset.ShipperV1alpha1().CapacityTargets(namespace).Update(initialCt)
			if err != nil {
				klog.Error(err)
				return err
			}
			continue
		}

		if !errors.IsAlreadyExists(err) {
			return err
		}
		// checking if existing object was migrated already
		capacityTarget, err := clusterClientsets.GetShipperClient().ShipperV1alpha1().CapacityTargets(ct.Namespace).Get(ct.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		migrationLabel, _ := objectutil.GetMigrationLabel(capacityTarget)
		if migrationLabel {
			klog.Infof("capacity target %s/%s already migrated", ct.Namespace, ct.Name)
			continue
		}
		// migrating object and updating object in application cluster with migrated label
		ct.ResourceVersion = initialCt.ResourceVersion
		ct.Labels[shipper.MigrationAnnotation] = "true"
		_, err = clusterClientsets.GetShipperClient().ShipperV1alpha1().CapacityTargets(ct.Namespace).Update(ct)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) migrateTrafficTargets(relName, namespace string, selector labels.Selector) error {
	gvk := shipper.SchemeGroupVersion.WithKind("TrafficTarget")
	// get target objects from mgmt cluster
	trafficTargets, err := c.trafficTargetLister.TrafficTargets(namespace).List(selector)
	if err != nil {
		if errors.IsNotFound(err) {
			// no trafficTargets for this release in mgmt cluster. moving on.
			return nil
		}
		return shippererrors.NewKubeclientListError(gvk, namespace, selector, err)
	}
	// there should be only one traffic target
	if len(trafficTargets) > 1 {
		return shippererrors.NewMultipleTargetObjectsForReleaseError(namespace, gvk.Kind, relName)
	}

	if len(trafficTargets) == 0 {
		// no objects to migrate
		return nil
	}

	// checking if we should migrate object
	initialTt := trafficTargets[0]
	migrationLabel, _ := objectutil.GetMigrationLabel(initialTt)
	if migrationLabel {
		// already migrated
		return nil
	}

	if initialTt.Spec.Clusters == nil || len(initialTt.Spec.Clusters) == 0 {
		// this traffic target doesn't have spec.clusters. moving on.
		return nil
	}

	klog.Infof("migrating traffic target for release %s/%s", namespace, relName)
	// create a new traffic target to put in application clusters
	tt := initialTt.DeepCopy()
	tt.Spec.Weight = initialTt.Spec.Clusters[0].Weight
	tt.ObjectMeta = metav1.ObjectMeta{
		Name:        initialTt.Name,
		Namespace:   initialTt.Namespace,
		Generation:  initialTt.Generation,
		Labels:      initialTt.Labels,
		Annotations: initialTt.Annotations,
	}

	// put in application clusters:
	for _, cluster := range initialTt.Spec.Clusters {
		clusterName := cluster.Name
		clusterClientsets, err := c.store.GetApplicationClusterClientset(clusterName, AgentName)
		if err != nil {
			return err
		}
		_, err = clusterClientsets.GetShipperClient().ShipperV1alpha1().TrafficTargets(tt.Namespace).Create(tt)
		if err == nil {
			// update initial object with migrated label
			initialTt.Labels[shipper.MigrationAnnotation] = "true"
			_, err = c.clientset.ShipperV1alpha1().TrafficTargets(namespace).Update(initialTt)
			if err != nil {
				klog.Error(err)
				return err
			}
			continue
		}

		if !errors.IsAlreadyExists(err) {
			return err
		}
		// checking if existing object was migrated already
		trafficTarget, err := clusterClientsets.GetShipperClient().ShipperV1alpha1().TrafficTargets(tt.Namespace).Get(tt.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		migrationLabel, _ := objectutil.GetMigrationLabel(trafficTarget)
		if migrationLabel {
			klog.Infof("traffic target %s/%s already migrated", tt.Namespace, tt.Name)
			continue
		}
		// migrating object and updating object in application cluster with migrated label
		tt.ResourceVersion = initialTt.ResourceVersion
		tt.Labels[shipper.MigrationAnnotation] = "true"
		_, err = clusterClientsets.GetShipperClient().ShipperV1alpha1().TrafficTargets(tt.Namespace).Update(tt)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) migrateInstallationTargets(relName, namespace string, selector labels.Selector) error {
	gvk := shipper.SchemeGroupVersion.WithKind("InstallationTarget")
	// get target objects from mgmt cluster
	installationTargets, err := c.installationTargetLister.InstallationTargets(namespace).List(selector)
	if err != nil {
		if errors.IsNotFound(err) {
			// no installationTargets for this release in mgmt cluster. moving on.
			return nil
		}
		return shippererrors.NewKubeclientListError(gvk, namespace, selector, err)
	}
	// there should be only one installation target
	if len(installationTargets) > 1 {
		return shippererrors.NewMultipleTargetObjectsForReleaseError(namespace, gvk.Kind, relName)
	}

	if len(installationTargets) == 0 {
		// no objects to migrate
		return nil
	}

	// checking if we should migrate object
	initialIt := installationTargets[0]
	migrationLabel, _ := objectutil.GetMigrationLabel(initialIt)
	if migrationLabel {
		// already migrated
		return nil
	}

	if initialIt.Spec.Clusters == nil || len(initialIt.Spec.Clusters) == 0 {
		// this installation target doesn't have spec.clusters. moving on.
		return nil
	}

	klog.Infof("migrating installation target for release %s/%s", namespace, relName)
	// create a new installation target to put in application clusters
	it := initialIt.DeepCopy()
	it.ObjectMeta = metav1.ObjectMeta{
		Name:        initialIt.Name,
		Namespace:   initialIt.Namespace,
		Generation:  initialIt.Generation,
		Labels:      initialIt.Labels,
		Annotations: initialIt.Annotations,
	}

	// put in application clusters:
	for _, clusterName := range initialIt.Spec.Clusters {
		clusterClientsets, err := c.store.GetApplicationClusterClientset(clusterName, AgentName)
		if err != nil {
			return err
		}
		_, err = clusterClientsets.GetShipperClient().ShipperV1alpha1().InstallationTargets(it.Namespace).Create(it)
		if err == nil {
			// update initial object with migrated label
			initialIt.Labels[shipper.MigrationAnnotation] = "true"
			_, err = c.clientset.ShipperV1alpha1().InstallationTargets(namespace).Update(initialIt)
			if err != nil {
				klog.Error(err)
				return err
			}
			continue
		}

		if !errors.IsAlreadyExists(err) {
			return err
		}
		// checking if existing object was migrated already
		capacityTarget, err := clusterClientsets.GetShipperClient().ShipperV1alpha1().InstallationTargets(it.Namespace).Get(it.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		migrationLabel, _ := objectutil.GetMigrationLabel(capacityTarget)
		if migrationLabel {
			klog.Infof("installation target %s/%s already migrated", it.Namespace, it.Name)
			continue
		}
		// updating object in application cluster with migrated label
		it.Labels[shipper.MigrationAnnotation] = "true"
		_, err = clusterClientsets.GetShipperClient().ShipperV1alpha1().InstallationTargets(it.Namespace).Update(it)
		if err != nil {
			return err
		}
	}
	return nil
}
