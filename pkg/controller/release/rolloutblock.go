package release

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
	rolloutblockUtil "github.com/bookingcom/shipper/pkg/util/rolloutblock"
)

func (s *Scheduler) shouldBlockRollout(rel *shipper.Release) (bool, error, string) {
	nsRBs, err := s.rolloutBlockLister.RolloutBlocks(rel.Namespace).List(labels.Everything())
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing Application %q Because of namespace RolloutBlocks (will retry): %s", rel.Name, err))
	}

	gbRBs, err := s.rolloutBlockLister.RolloutBlocks(shipper.ShipperNamespace).List(labels.Everything())
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing Application %q Because of global RolloutBlocks (will retry): %s", rel.Name, err))
	}

	appName, err := releaseutil.ApplicationNameForRelease(rel)
	if err != nil {
		return true, err, ""
	}
	app, err := s.applicationLister.Applications(rel.Namespace).Get(appName)
	if err != nil {
		return true, err, ""
	}


	overrideRB, ok := app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		overrideRB = ""
	}

	overrideRolloutBlock, eventMessage, err := rolloutblockUtil.ShouldOverrideRolloutBlock(overrideRB, nsRBs, gbRBs)
	if err != nil {
		switch err.(type) {
		case shippererrors.InvalidRolloutBlockOverrideError:
			err = nil
		default:
			s.recorder.Event(rel, corev1.EventTypeWarning, "Overriding RolloutBlock", err.Error())
			runtime.HandleError(fmt.Errorf("error overriding rollout block %s", err.Error()))
			return true, err, ""
		}
	}

	if overrideRolloutBlock && len(overrideRB) > 0 {
		s.recorder.Event(rel, corev1.EventTypeNormal, "Override RolloutBlock", overrideRB)
	}

	return !overrideRolloutBlock, err, eventMessage
}