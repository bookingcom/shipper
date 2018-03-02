package cache

import (
	"errors"
)

var ErrClusterNotReady error = errors.New("ErrClusterClientNotReadyForUse")
var ErrClusterNotInStore error = errors.New("ErrClusterNotInStore")
