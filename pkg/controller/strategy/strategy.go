package strategy

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/runtime/schema"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

type ExecutorResult interface {
	PatchSpec() (string, schema.GroupVersionKind, []byte)
}

type CapacityTargetOutdatedResult struct {
	Name    string
	NewSpec *shipper.CapacityTargetSpec
}

type TrafficTargetOutdatedResult struct {
	Name    string
	NewSpec *shipper.TrafficTargetSpec
}

type ReleaseUpdateResult struct {
	Name      string
	NewStatus *shipper.ReleaseStatus
}

func (c *CapacityTargetOutdatedResult) PatchSpec() (string, schema.GroupVersionKind, []byte) {
	patch := make(map[string]interface{})
	patch["spec"] = c.NewSpec
	b, _ := json.Marshal(patch)
	return c.Name, schema.GroupVersionKind{
		Group:   shipper.SchemeGroupVersion.Group,
		Version: shipper.SchemeGroupVersion.Version,
		Kind:    "CapacityTarget",
	}, b
}

func (c *TrafficTargetOutdatedResult) PatchSpec() (string, schema.GroupVersionKind, []byte) {
	patch := make(map[string]interface{})
	patch["spec"] = c.NewSpec
	b, _ := json.Marshal(patch)
	return c.Name, schema.GroupVersionKind{
		Group:   shipper.SchemeGroupVersion.Group,
		Version: shipper.SchemeGroupVersion.Version,
		Kind:    "TrafficTarget",
	}, b
}

func (r *ReleaseUpdateResult) PatchSpec() (string, schema.GroupVersionKind, []byte) {
	patch := make(map[string]interface{})
	patch["status"] = r.NewStatus
	b, _ := json.Marshal(patch)
	return r.Name, schema.GroupVersionKind{
		Group:   shipper.SchemeGroupVersion.Group,
		Version: shipper.SchemeGroupVersion.Version,
		Kind:    "Release",
	}, b
}
