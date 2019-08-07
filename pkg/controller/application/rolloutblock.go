package application

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	apputil "github.com/bookingcom/shipper/pkg/util/application"
	"github.com/bookingcom/shipper/pkg/util/rolloutblock"
)

func (c *Controller) processRolloutBlocks(app *shipper.Application, nsRBs, gbRBs []*shipper.RolloutBlock) bool {
	appOverrideRBs := rolloutblock.NewObjectNameList(app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation])
	rbs := append(nsRBs, gbRBs...)

	existingRBs := rolloutblock.NewObjectNameListFromRolloutBlocksList(rbs)
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
		c.recorder.Event(app, corev1.EventTypeNormal, "RolloutBlockOverriden", appOverrideRBs.String())
	}

	return shouldBlockRollout
}

func (c *Controller) removeRolloutBlockFromAnnotations(overrideRBs rolloutblock.ObjectNameList, rbName string, app *shipper.Application) {
	overrideRBs.Delete(rbName)

	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = overrideRBs.String()
}

func (c *Controller) updateApplicationRolloutBlockCondition(rbs []*shipper.RolloutBlock, app *shipper.Application) {
	if len(rbs) > 0 {
		existingRolloutBlocks := fmt.Sprintf("rollouts blocked by: %s", rolloutblock.NewObjectNameListFromRolloutBlocksList(rbs).String())
		rolloutBlockCond := apputil.NewApplicationCondition(shipper.ApplicationConditionTypeBlocked, corev1.ConditionTrue, shipper.RolloutBlockReason, existingRolloutBlocks)
		apputil.SetApplicationCondition(&app.Status, *rolloutBlockCond)
	} else {
		rolloutBlockCond := apputil.NewApplicationCondition(shipper.ApplicationConditionTypeBlocked, corev1.ConditionFalse, "", "")
		apputil.SetApplicationCondition(&app.Status, *rolloutBlockCond)
	}
}
