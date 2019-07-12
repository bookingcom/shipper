package rolloutblock

import (
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	rolloutBlockOverride "github.com/bookingcom/shipper/pkg/util/rolloutblock"
)

func (c *Controller) addApplicationsToRolloutBlocks(rolloutBlockKey string, rolloutBlock *shipper.RolloutBlock, applications ...*shipper.Application) error {
	appsKeys := rolloutBlockOverride.NewOverride("")
	for _, app := range applications {
		if app.DeletionTimestamp != nil {
			continue
		}

		appKey, err := cache.MetaNamespaceKeyFunc(app)
		if err != nil {
			runtime.HandleError(err)
			continue
		}

		overrideRB, ok := app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
		if !ok {
			continue
		}

		overrideRBs := rolloutBlockOverride.NewOverride(overrideRB)
		for rbKey := range overrideRBs {
			if rbKey == rolloutBlockKey {
				appsKeys.Add(appKey)
			}
		}
	}

	rolloutBlock.Status.Overrides.Application = appsKeys.String()

	return nil
}
