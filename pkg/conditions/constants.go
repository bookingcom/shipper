package conditions

const (
	// Operational
	ServerError = "ServerError"

	// Capacity Ready
	MissingDeployment  = "MissingDeployment"
	TooManyDeployments = "TooManyDeployments"
	PodsNotReady       = "PodsNotReady"
	WrongPodCount      = "WrongPodCount"

	MissingObjects = "MissingObjects"
	InvalidObjects = "InvalidObjects"

	MissingService = "MissingService"

	UnknownError = "UnknownError"

	InternalError = "InternalError"

	TargetClusterClientError = "TargetClusterClientError"
)
