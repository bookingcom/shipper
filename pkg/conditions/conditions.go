package conditions

const (
	ClustersNotOperational = "ClustersNotOperational"
	ClustersNotReady       = "ClustersNotReady"
)

// TODO(asurikov): change NotFound to be a struct that implements error.

type NotFound error
