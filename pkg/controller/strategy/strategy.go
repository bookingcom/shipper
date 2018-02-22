package strategy

import (
	"encoding/json"
	"github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type ExecutorResult interface {
	PatchSpec() (string, schema.GroupVersionKind, []byte)
}

type CapacityTargetOutdatedResult struct {
	Name    string
	NewSpec *v1.CapacityTargetSpec
}

type TrafficTargetOutdatedResult struct {
	Name    string
	NewSpec *v1.TrafficTargetSpec
}

type ReleaseUpdateResult struct {
	Name      string
	NewStatus *v1.ReleaseStatus
}

func (c *CapacityTargetOutdatedResult) PatchSpec() (string, schema.GroupVersionKind, []byte) {
	patch := make(map[string]interface{})
	patch["spec"] = c.NewSpec
	b, _ := json.Marshal(patch)
	return c.Name, schema.GroupVersionKind{Group: "shipper.booking.com", Version: "v1", Kind: "CapacityTarget"}, b
}

func (c *TrafficTargetOutdatedResult) PatchSpec() (string, schema.GroupVersionKind, []byte) {
	patch := make(map[string]interface{})
	patch["spec"] = c.NewSpec
	b, _ := json.Marshal(patch)
	return c.Name, schema.GroupVersionKind{Group: "shipper.booking.com", Version: "v1", Kind: "TrafficTarget"}, b
}

func (r *ReleaseUpdateResult) PatchSpec() (string, schema.GroupVersionKind, []byte) {
	patch := make(map[string]interface{})
	patch["status"] = r.NewStatus
	b, _ := json.Marshal(patch)
	return r.Name, schema.GroupVersionKind{Group: "shipper.booking.com", Version: "v1", Kind: "Release"}, b
}
