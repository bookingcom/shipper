package rolloutblock

import "fmt"

const (
	OverridingApplication = "OverridingApplication"
	OverridingRelease     = "OverridingRelease"
	NewRolloutBlockObject = "NewRolloutBlockObject"
)

// A helper struct for updating a rolloutblock status with overriding objects.
type RolloutBlockUpdater struct {
	RolloutBlockKey     string // the RolloutBlock object being overridden
	UpdaterType         string // the object type that overrides
	OverridingObjectKey string // the object key that overrides
	IsDeletedObject     bool   // true if the overriding object is being deleted
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
