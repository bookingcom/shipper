package application

import (
	corev1 "k8s.io/api/core/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	apputil "github.com/bookingcom/shipper/pkg/util/application"
	"github.com/bookingcom/shipper/pkg/util/rolloutblock"
)

func (c *Controller) processRolloutBlocks(app *shipper.Application, nsRBs, gbRBs []*shipper.RolloutBlock) bool {
	appOverrideRBs := rolloutblock.NewOverride(app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation])
	rbs := append(nsRBs, gbRBs...)

	existingRBs := rolloutblock.NewOverrideFromRolloutBlocks(rbs)
	obsoleteRbs := appOverrideRBs.Diff(existingRBs)

	if len(obsoleteRbs) > 0 {
		for o := range obsoleteRbs {
			c.removeRolloutBlockFromAnnotations(appOverrideRBs, o, app)
		}
		c.updateApplicationRolloutBlockCondition(rbs, app)
		c.recorder.Event(app, corev1.EventTypeWarning, "Non Existing RolloutBlock", obsoleteRbs.String())
	}

	nonOverriddenRBs := existingRBs.Diff(appOverrideRBs)
	shouldBlockRollout := len(nonOverriddenRBs) != 0

	if shouldBlockRollout {
		c.recorder.Event(app, corev1.EventTypeWarning, "RolloutBlock", nonOverriddenRBs.String())
	} else if len(appOverrideRBs) > 0 {
		c.recorder.Event(app, corev1.EventTypeNormal, "Overriding RolloutBlock", appOverrideRBs.String())
	}

	return shouldBlockRollout
}

func (c *Controller) removeRolloutBlockFromAnnotations(overrideRBs rolloutblock.Override, rbName string, app *shipper.Application) {
	overrideRBs.Delete(rbName)

	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = overrideRBs.String()
}

func (c *Controller) updateApplicationRolloutBlockCondition(rbs []*shipper.RolloutBlock, app *shipper.Application) {
	if len(rbs) > 0 {
		existingRolloutBlocks := rolloutblock.NewOverrideFromRolloutBlocks(rbs)
		rolloutBlockCond := apputil.NewApplicationCondition(shipper.ApplicationConditionTypeRolloutBlock, corev1.ConditionTrue, existingRolloutBlocks.String(), "")
		apputil.SetApplicationCondition(&app.Status, *rolloutBlockCond)
	} else {
		rolloutBlockCond := apputil.NewApplicationCondition(shipper.ApplicationConditionTypeRolloutBlock, corev1.ConditionFalse, "", "")
		apputil.SetApplicationCondition(&app.Status, *rolloutBlockCond)
	}
}
