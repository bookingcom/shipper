package release

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/util/rolloutblock"
)

func (s *Scheduler) processRolloutBlocks(rel *shipper.Release) (bool, string) {
	relOverrideRBs := rolloutblock.NewOverride(rel.Annotations[shipper.RolloutBlocksOverrideAnnotation])

	nsRBs, err := s.rolloutBlockLister.RolloutBlocks(rel.Namespace).List(labels.Everything())
	if err != nil {
		runtime.HandleError(fmt.Errorf("failed to list rollout block objects: %s", err))
	}

	gbRBs, err := s.rolloutBlockLister.RolloutBlocks(shipper.GlobalRolloutBlockNamespace).List(labels.Everything())
	if err != nil {
		runtime.HandleError(fmt.Errorf("failed to list rollout block objects: %s", err))
	}

	existingRBs := rolloutblock.NewOverrideFromRolloutBlocks(append(nsRBs, gbRBs...))
	obsoleteRbs := relOverrideRBs.Diff(existingRBs)

	if len(obsoleteRbs) > 0 {
		for o := range obsoleteRbs {
			s.removeRolloutBlockFromAnnotations(relOverrideRBs, o, rel)
		}
		s.recorder.Event(rel, corev1.EventTypeWarning, "Non Existing RolloutBlock", obsoleteRbs.String())
	}

	nonOverriddenRBs := existingRBs.Diff(relOverrideRBs)
	shouldBlockRollout := len(nonOverriddenRBs) != 0
	nonOverriddenRBsStatement := nonOverriddenRBs.String()

	if shouldBlockRollout {
		s.recorder.Event(rel, corev1.EventTypeWarning, "RolloutBlock", nonOverriddenRBsStatement)
	} else if len(relOverrideRBs) > 0 {
		s.recorder.Event(rel, corev1.EventTypeNormal, "Overriding RolloutBlock", relOverrideRBs.String())
	}

	return shouldBlockRollout, nonOverriddenRBsStatement
}

func (s *Scheduler) removeRolloutBlockFromAnnotations(overrideRBs rolloutblock.Override, rbName string, release *shipper.Release) {
	overrideRBs.Delete(rbName)
	release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = overrideRBs.String()
}
