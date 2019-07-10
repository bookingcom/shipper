package application

import (
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	apputil "github.com/bookingcom/shipper/pkg/util/application"
	rolloutBlockOverride "github.com/bookingcom/shipper/pkg/util/rolloutblock"
	corev1 "k8s.io/api/core/v1"
)

func (c *Controller) processRolloutBlocks(app *shipper.Application, nsRBs, gbRBs []*shipper.RolloutBlock) bool {
	overrideRB, ok := app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		overrideRB = ""
	}
	appOverrideRBs := rolloutBlockOverride.NewOverride(overrideRB)
	rbs := append(nsRBs, gbRBs...)
	existingRBs := rolloutBlockOverride.NewOverrideFromRolloutBlocks(rbs)
	nonExistingRbs := appOverrideRBs.Diff(existingRBs)
	if len(nonExistingRbs) > 0 {
		for o := range nonExistingRbs {
			c.removeRolloutBlockFromAnnotations(appOverrideRBs, o, app)
		}
		c.updateApplicationRolloutBlockCondition(rbs, app)
		c.recorder.Event(app, corev1.EventTypeWarning, "Non Existing RolloutBlock", nonExistingRbs.String())
	}
	nonOverriddenRBs := existingRBs.Diff(appOverrideRBs)
	shouldBlockRollout := len(nonOverriddenRBs) != 0

	if shouldBlockRollout {
		c.recorder.Event(app, corev1.EventTypeWarning, "RolloutBlock", nonOverriddenRBs.String())
	} else if len(overrideRB) > 0 {
		c.recorder.Event(app, corev1.EventTypeNormal, "Overriding RolloutBlock", overrideRB)
	}
	return shouldBlockRollout
}

func (c *Controller) removeRolloutBlockFromAnnotations(overrideRBs rolloutBlockOverride.Override, rbName string, app *shipper.Application) {
	overrideRBs.Delete(rbName)

	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = overrideRBs.String()
}

func (c *Controller) updateApplicationRolloutBlockCondition(rbs []*shipper.RolloutBlock, app *shipper.Application) {
	if len(rbs) > 0 {
		existingRolloutBlocks := rolloutBlockOverride.NewOverrideFromRolloutBlocks(rbs)
		rolloutBlockCond := apputil.NewApplicationCondition(shipper.ApplicationConditionTypeRolloutBlock, corev1.ConditionTrue, existingRolloutBlocks.String(), "")
		apputil.SetApplicationCondition(&app.Status, *rolloutBlockCond)
	} else {
		rolloutBlockCond := apputil.NewApplicationCondition(shipper.ApplicationConditionTypeRolloutBlock, corev1.ConditionFalse, "", "")
		apputil.SetApplicationCondition(&app.Status, *rolloutBlockCond)
	}
}
