package release

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	rolloutBlockOverride "github.com/bookingcom/shipper/pkg/util/rolloutblock"
)

func (s *Scheduler) shouldBlockRollout(rel *shipper.Release) (bool, string, error) {
	relOverrideRB, ok := rel.Annotations[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		relOverrideRB = ""
	}

	nsRBs, err := s.rolloutBlockLister.RolloutBlocks(rel.Namespace).List(labels.Everything())
	if err != nil {
		runtime.HandleError(fmt.Errorf("failed to list rollout block objects: %s", err))
	}

	gbRBs, err := s.rolloutBlockLister.RolloutBlocks(shipper.GlobalRolloutBlockNamespace).List(labels.Everything())
	if err != nil {
		runtime.HandleError(fmt.Errorf("failed to list rollout block objects: %s", err))
	}

	rbs := append(nsRBs, gbRBs...)
	overrideRolloutBlock, eventMessage, err := rolloutBlockOverride.ShouldOverride(relOverrideRB, rbs)
	if err != nil {
		switch errT := err.(type) {
		case shippererrors.InvalidRolloutBlockOverrideError:
			// remove from annotation!
			rbName := err.(shippererrors.InvalidRolloutBlockOverrideError).RolloutBlockName
			s.removeRolloutBlockFromAnnotations(relOverrideRB, rbName, rel)
		default:
			s.recorder.Event(rel, corev1.EventTypeWarning, "Overriding RolloutBlock", err.Error())
			runtime.HandleError(fmt.Errorf("error of type %T overriding rollout block %s", errT, err.Error()))
			return true, "", err
		}
	}

	if overrideRolloutBlock && len(relOverrideRB) > 0 {
		s.recorder.Event(rel, corev1.EventTypeNormal, "Override RolloutBlock", relOverrideRB)
	}

	return !overrideRolloutBlock, eventMessage, nil
}

func (s *Scheduler) removeRolloutBlockFromAnnotations(overrideRB string, rbName string, release *shipper.Release) {
	overrideRBs := rolloutBlockOverride.NewOverride(overrideRB)
	overrideRBs.Delete(rbName)
	release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = overrideRBs.String()
	_, err := s.clientset.ShipperV1alpha1().Releases(release.Namespace).Update(release)
	if err != nil {
		runtime.HandleError(err)
	}
}