package traffic

import (
	"k8s.io/client-go/kubernetes"
)

type TrafficShifter interface {
	Clusters() []string
	SyncCluster(string, kubernetes.Interface) []error
}
