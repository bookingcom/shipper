package conditions

const (
	ServerError              = "ServerError"
	TargetClusterClientError = "TargetClusterClientError"

	CreateReleaseFailed                 = "CreateReleaseFailed"
	ChartVersionResolutionFailed        = "ChartVersionResolutionFailed"
	BrokenReleaseGeneration             = "BrokenReleaseGeneration"
	BrokenApplicationObservedGeneration = "BrokenApplicationObservedGeneration"
	StrategyExecutionFailed             = "StrategyExecutionFailed"
)
