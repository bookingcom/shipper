package rolloutblock

import "fmt"

const (
	OverridingApplication = "OverridingApplication"
	OverridingRelease     = "OverridingRelease"
	NewRolloutBlockObject = "NewRolloutBlockObject"
)

type RolloutBlockUpdater struct {
	RolloutBlockKey     string
	UpdaterType         string
	OverridingObjectKey string
	IsDeletedObject     bool
}

func (r RolloutBlockUpdater) String() string {
	return fmt.Sprintf(
		"UpdaterType: %s, RolloutBlockKey: %s, OverridingObjectKey: %s, IsDeletedObject? %t",
		r.UpdaterType,
		r.RolloutBlockKey,
		r.OverridingObjectKey,
		r.IsDeletedObject,
		)

}
