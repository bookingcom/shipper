package rolloutblock

import (
	"github.com/golang/glog"
	"strings"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

func ShouldOverrideRolloutBlock(overrideRB string, nsRBs []*shipper.RolloutBlock, gbRBs []*shipper.RolloutBlock) (bool, string, error) {
	overrideRBs := strings.Split(overrideRB, ",")
	RBs := append(nsRBs, gbRBs...)

	nonOverriddenRBs, err := difference(RBs, overrideRBs)

	return len(nonOverriddenRBs) == 0, strings.Join(nonOverriddenRBs, ", "), err
}

// Set difference: existingRBs - overrideRBs
// finding all RolloutBlocks that are not overridden
// making sure all overrides are valid rolloutblock objects
func difference(existingRBs []*shipper.RolloutBlock, overrideRBs []string) ([]string, error) {
	var diff []string
	overrideRBdict := make(map[string]bool)
	existingRBdict := make(map[string]bool)

	for _, item := range overrideRBs {
		overrideRBdict[item] = true
	}

	// Collecting rolloutblocks that exist but are not overridden
	for _, item := range existingRBs {
		rbFullName := item.Namespace + "/" + item.Name
		existingRBdict[rbFullName] = true
		if _, ok := overrideRBdict[rbFullName]; !ok {
			diff = append(diff, rbFullName)
		}
	}

	// Non-existing blocks enlisted in this annotation should not be allowed.
	for _, item := range overrideRBs {
		if _, ok := existingRBdict[item]; !ok {
			glog.Infof("Claiming this rollout block %s does not exists!!! 77777", item)
			return diff, shippererrors.NewInvalidRolloutBlockOverrideError(item)
		}
	}

	return diff, nil
}
