package conditions

const (
	// Operational.
	ServerError = "ServerError"

	// Capacity Ready.
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

	CreateReleaseFailed                 = "CreateReleaseFailed"
	FetchReleaseFailed                  = "FetchReleaseFailed"
	BrokenReleaseGeneration             = "BrokenReleaseGeneration"
	BrokenApplicationObservedGeneration = "BrokenApplicationObservedGeneration"
	StrategyExecutionFailed             = "StrategyExecutionFailed"

	ChartError  = "ChartError"
	ClientError = "ClientError"
)
