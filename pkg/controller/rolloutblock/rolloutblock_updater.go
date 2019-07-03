package rolloutblock

type UpdaterType uint8

const (
	UndefinedUpdater UpdaterType = iota
	DeletedApplicationUpdater
	AddedApplicationUpdater
	DeletedReleaseUpdater
	AddedReleaseUpdater
	NewRolloutBlockUpdater
)

// A helper struct for updating a rolloutblock status with overriding objects.
type RolloutBlockUpdater struct {
	RolloutBlockKey     string      // the RolloutBlock object being overridden
	ObjectType          UpdaterType // the object type that overrides
	OverridingObjectKey string      // the object key that overrides
}
