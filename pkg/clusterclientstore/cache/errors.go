package cache

import (
	"errors"
)

var ErrClusterNotReady error = errors.New("cluster not ready yet for use; cluster client is being initialized")
var ErrClusterNotInStore error = errors.New("cluster unknown; please check if cluster exists in management cluster")
