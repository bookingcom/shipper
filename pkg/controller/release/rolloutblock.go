package release

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	rolloutblockUtil "github.com/bookingcom/shipper/pkg/util/rolloutblock"
	stringUtil "github.com/bookingcom/shipper/pkg/util/string"
)

func (s *Scheduler) shouldBlockRollout(rel *shipper.Release) (bool, string, error) {
	relOverrideRB, ok := rel.Annotations[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		relOverrideRB = ""
	}

	nsRBs, err := s.rolloutBlockLister.RolloutBlocks(rel.Namespace).List(labels.Everything())
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing Release %q Because of namespace RolloutBlocks (will retry): %s", rel.Name, err))
	}

	gbRBs, err := s.rolloutBlockLister.RolloutBlocks(shipper.GlobalRolloutBlockNamespace).List(labels.Everything())
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing Release %q Because of global RolloutBlocks (will retry): %s", rel.Name, err))
	}

	overrideRolloutBlock, eventMessage, err := rolloutblockUtil.ShouldOverrideRolloutBlock(relOverrideRB, nsRBs, gbRBs)
	if err != nil {
		switch errT := err.(type) {
		case shippererrors.InvalidRolloutBlockOverrideError:
			// remove from annotation!
			rbName := err.(shippererrors.InvalidRolloutBlockOverrideError).RolloutBlockName
			s.removeRolloutBlockFromAnnotations(relOverrideRB, rbName, rel)
			err = nil
		default:
			s.recorder.Event(rel, corev1.EventTypeWarning, "Overriding RolloutBlock", err.Error())
			runtime.HandleError(fmt.Errorf("error of type %T overriding rollout block %s", errT, err.Error()))
			return true, "", err
		}
	}

	if overrideRolloutBlock && len(relOverrideRB) > 0 {
		s.recorder.Event(rel, corev1.EventTypeNormal, "Override RolloutBlock", relOverrideRB)
	}

	return !overrideRolloutBlock, eventMessage, err
}

func (s *Scheduler) removeRolloutBlockFromAnnotations(overrideRB string, rbName string, release *shipper.Release) {
	overrideRBs := strings.Split(overrideRB, ",")
	overrideRBs = stringUtil.Delete(overrideRBs, rbName)
	sort.Slice(overrideRBs, func(i, j int) bool {
		return overrideRBs[i] < overrideRBs[j]
	})
	release.Annotations[shipper.RolloutBlocksOverrideAnnotation] = strings.Join(overrideRBs, ",")
	_, err := s.clientset.ShipperV1alpha1().Releases(release.Namespace).Update(release)
	if err != nil {
		runtime.HandleError(err)
	}
}