package application

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/runtime"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	apputil "github.com/bookingcom/shipper/pkg/util/application"
	rolloutblockUtil "github.com/bookingcom/shipper/pkg/util/rolloutblock"
	stringUtil "github.com/bookingcom/shipper/pkg/util/string"
)

func (c *Controller) shouldBlockRollout(app *shipper.Application, nsRBs, gbRBs []*shipper.RolloutBlock) bool {
	overrideRB, ok := app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		overrideRB = ""
	}

	overrideRolloutBlock, eventMessage, err := rolloutblockUtil.ShouldOverrideRolloutBlock(overrideRB, nsRBs, gbRBs)
	if err != nil {
		switch err.(type) {
		case shippererrors.InvalidRolloutBlockOverrideError:
			// remove from annotation!
			rbName := err.(shippererrors.InvalidRolloutBlockOverrideError).RolloutBlockName
			c.removeRolloutBlockFromAnnotations(overrideRB, rbName, app)
			c.updateApplicationRolloutBlockCondition(append(nsRBs, gbRBs...), app)
		default:
			runtime.HandleError(fmt.Errorf("error overriding rollout block %s", err.Error()))
		}
	}

	if !overrideRolloutBlock {
		c.recorder.Event(app, corev1.EventTypeWarning, "RolloutBlock", eventMessage)
	} else if len(overrideRB) > 0 {
		c.recorder.Event(app, corev1.EventTypeNormal, "Overriding RolloutBlock", overrideRB)
	}
	return !overrideRolloutBlock
}

func (c *Controller) removeRolloutBlockFromAnnotations(overrideRB string, rbName string, app *shipper.Application) {
	overrideRBs := strings.Split(overrideRB, ",")
	overrideRBs = stringUtil.Delete(overrideRBs, rbName)
	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = strings.Join(overrideRBs, ",")
	_, err := c.shipperClientset.ShipperV1alpha1().Applications(app.Namespace).Update(app)
	if err != nil {
		runtime.HandleError(err)
	}
}

func (c *Controller) updateApplicationRolloutBlockCondition(rbs []*shipper.RolloutBlock, app *shipper.Application) {
	if len(rbs) > 0 {
		var sb strings.Builder
		for _, rb := range rbs {
			sb.WriteString(rb.Name + " ")
		}
		rolloutBlockCond := apputil.NewApplicationCondition(shipper.ApplicationConditionTypeRolloutBlock, corev1.ConditionTrue, sb.String(), "")
		apputil.SetApplicationCondition(&app.Status, *rolloutBlockCond)
	} else {
		rolloutBlockCond := apputil.NewApplicationCondition(shipper.ApplicationConditionTypeRolloutBlock, corev1.ConditionFalse, "", "")
		apputil.SetApplicationCondition(&app.Status, *rolloutBlockCond)
	}
}
