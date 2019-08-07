package rolloutblock

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	"github.com/bookingcom/shipper/pkg/util/rolloutblock"
)

func (c *Controller) addApplicationsToRolloutBlocks(rolloutBlockKey string, rolloutBlock *shipper.RolloutBlock, applications ...*shipper.Application) error {
	appsKeys := rolloutblock.NewObjectNameList("")
	for _, app := range applications {
		if app.DeletionTimestamp != nil {
			continue
		}

		appKey, err := cache.MetaNamespaceKeyFunc(app)
		if err != nil {
			runtime.HandleError(err)
			continue
		}

		rbOverrideAnnotation, ok := app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
		if !ok {
			continue
		}

		overrideRBs := rolloutblock.NewObjectNameList(rbOverrideAnnotation)
		for rbKey := range overrideRBs {
			if rbKey == rolloutBlockKey {
				appsKeys.Add(appKey)
			}
		}
	}

	rolloutBlock.Status.Overrides.Application = appsKeys.String()

	return nil
}

func (c *Controller) syncApplication(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	app, err := c.applicationLister.Applications(ns).Get(name)
	if err != nil {
		return err
	}
	overrides := rolloutblock.NewObjectNameList(app.Annotations[shipper.RolloutBlocksOverrideAnnotation])
	for rbkey := range overrides {
		rbNs, rbName, err := cache.SplitMetaNamespaceKey(rbkey)
		if err != nil {
			continue
		}
		_, err = c.rolloutBlockLister.RolloutBlocks(rbNs).Get(rbName)
		if errors.IsNotFound(err) {
			overrides.Delete(rbkey)
		}
	}
	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = overrides.String()
	_, err = c.shipperClientset.ShipperV1alpha1().Applications(ns).Update(app)
	if err != nil {
		return shippererrors.NewKubeclientUpdateError(app, err).
			WithShipperKind("Application")
	}

	return nil
}
