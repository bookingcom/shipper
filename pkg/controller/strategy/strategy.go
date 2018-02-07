package strategy

import (
	"encoding/json"
	"github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type TargetState int

const (
	TargetStateAchieved TargetState = iota
	TargetStatePending
	TargetStateOutdated
)

type ExecutorResult interface {
	Patch() (schema.GroupVersionKind, []byte)
}

type CapacityTargetOutdatedResult struct {
	NewSpec *v1.CapacityTargetSpec
}

type TrafficTargetOutdatedResult struct {
	NewSpec *v1.TrafficTargetSpec
}

type ReleaseUpdateResult struct {
	NewStatus *v1.ReleaseStatus
}

func (c *CapacityTargetOutdatedResult) Patch() (schema.GroupVersionKind, []byte) {
	b, _ := json.Marshal(c.NewSpec)
	return (&v1.CapacityTarget{}).GroupVersionKind(), b
}

func (c *TrafficTargetOutdatedResult) Patch() (schema.GroupVersionKind, []byte) {
	b, _ := json.Marshal(c.NewSpec)
	return (&v1.TrafficTarget{}).GroupVersionKind(), b
}

func (r *ReleaseUpdateResult) Patch() (schema.GroupVersionKind, []byte) {
	b, _ := json.Marshal(r.NewStatus)
	return (&v1.Release{}).GroupVersionKind(), b
}
