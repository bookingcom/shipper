package release

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
	Name              string
	NewStrategyStatus *shipper.ReleaseStrategyStatus
}

type ReleaseAnnotationUpdateResult struct {
	Name           string
	NewAnnotations map[string]string
}

func (c *CapacityTargetOutdatedResult) PatchSpec() (string, schema.GroupVersionKind, []byte) {
	patch := make(map[string]interface{})
	patch["spec"] = c.NewSpec
	b, _ := json.Marshal(patch)
	return c.Name, shipper.SchemeGroupVersion.WithKind("CapacityTarget"), b
}

func (c *TrafficTargetOutdatedResult) PatchSpec() (string, schema.GroupVersionKind, []byte) {
	patch := make(map[string]interface{})
	patch["spec"] = c.NewSpec
	b, _ := json.Marshal(patch)
	return c.Name, shipper.SchemeGroupVersion.WithKind("TrafficTarget"), b
}

func (r *ReleaseUpdateResult) PatchSpec() (string, schema.GroupVersionKind, []byte) {
	patch := map[string]interface{}{
		"status": map[string]interface{}{
			"strategy": r.NewStrategyStatus,
		},
	}
	b, _ := json.Marshal(patch)
	return r.Name, shipper.SchemeGroupVersion.WithKind("Release"), b
}
