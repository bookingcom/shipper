package rolloutblock

import (
	"strings"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func ShouldOverrideRolloutBlock(app *shipper.Application, nsRBs []*shipper.RolloutBlock, gbRBs []*shipper.RolloutBlock) (bool, string) {
	// check annotations for rollout block override

	overrideRB, ok := app.Annotations[shipper.AppOverrideRolloutBlocksAnnotation]
	if !ok {
		return false, ""
	}

	overrideRBs := strings.Split(overrideRB, ",")
	RBs := append(nsRBs, gbRBs...)

	nonOverriddenRBs := Difference(RBs, overrideRBs)

	return len(nonOverriddenRBs) == 0, strings.Join(nonOverriddenRBs, ", ")
}

// Set Difference: existingRBs - overrideRBs
// finding all RolloutBlocks that are not overridden
func Difference(existingRBs []*shipper.RolloutBlock, overrideRBs []string) (diff []string) {
	m := make(map[string]bool)

	for _, item := range overrideRBs {
		m[item] = true
	}

	for _, item := range existingRBs {
		if _, ok := m[item.Name]; !ok {
			diff = append(diff, item.Name)
		}
	}
	return
}