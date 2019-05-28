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

	diff := Difference(RBs, overrideRBs)

	return len(diff) == 0, strings.Join(diff, ", ")
}

// Set Difference: A - B
func Difference(a []*shipper.RolloutBlock, b []string) (diff []string) {
	m := make(map[string]bool)

	for _, item := range b {
		m[item] = true
	}

	for _, item := range a {
		if _, ok := m[item.Name]; !ok {
			diff = append(diff, item.Name)
		}
	}
	return
}