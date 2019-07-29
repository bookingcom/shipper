package rolloutblock

import (
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func ProcessRolloutBlocks(overrideRbs Override, rbs []*shipper.RolloutBlock) (bool, string, Override) {

	existingRBs := NewOverrideFromRolloutBlocks(rbs)
	obsoleteRbs := overrideRbs.Diff(existingRBs)
	//if len(obsoleteRbs) > 0 {
	//	for o := range obsoleteRbs {
	//		s.removeRolloutBlockFromAnnotations(overrideRbs, o, rel)
	//	}
	//	s.recorder.Event(rel, corev1.EventTypeWarning, "Non Existing RolloutBlock", obsoleteRbs.String())
	//}

	nonOverriddenRBs := existingRBs.Diff(overrideRbs)
	shouldBlockRollout := len(nonOverriddenRBs) != 0
	nonOverriddenRBsStatement := nonOverriddenRBs.String()

	//if shouldBlockRollout {
	//	s.recorder.Event(rel, corev1.EventTypeWarning, "RolloutBlock", nonOverriddenRBsStatement)
	//} else if len(rbOverrideAnnotation) > 0 {
	//	s.recorder.Event(rel, corev1.EventTypeNormal, "Overriding RolloutBlock", rbOverrideAnnotation)
	//}

	return shouldBlockRollout, nonOverriddenRBsStatement, obsoleteRbs
}