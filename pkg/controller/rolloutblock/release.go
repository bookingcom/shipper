package rolloutblock

import (
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	rolloutBlockOverride "github.com/bookingcom/shipper/pkg/util/rolloutblock"
)

func (c *Controller) addReleasesToRolloutBlocks(rolloutBlockKey string, rolloutBlock *shipper.RolloutBlock, releases ...*shipper.Release) error {
	relsKeys := rolloutBlockOverride.NewOverride("")
	for _, release := range releases {
		if release.DeletionTimestamp != nil {
			continue
		}

		relKey, err := cache.MetaNamespaceKeyFunc(release)
		if err != nil {
			runtime.HandleError(err)
			continue
		}

		overrideRB, ok := release.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
		if !ok {
			continue
		}

		overrideRBs := rolloutBlockOverride.NewOverride(overrideRB)
		for rbKey := range overrideRBs {
			if rbKey == rolloutBlockKey {
				relsKeys.Add(relKey)
			}
		}
	}

	rolloutBlock.Status.Overrides.Release = relsKeys.String()

	return nil
}
